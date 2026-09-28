package manuales

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/gcs"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"gorm.io/gorm"
)

// === CONTROL DE ACCESO ROBUSTO ===

// isUserAdmin verifica de forma óptima si el usuario tiene rol de Super Admin o el permiso admin_biblioteca
func isUserAdmin(c *fiber.Ctx) bool {
	claims, ok := c.Locals("userClaims").(jwt.MapClaims)
	if !ok {
		return false
	}

	// 1. Verificar roles en Claims (Token)
	if rolesRaw, ok := claims["roles"]; ok {
		switch r := rolesRaw.(type) {
		case []interface{}:
			for _, role := range r {
				if s, ok := role.(string); ok && (s == "Super Admin" || s == "Administrador" || s == "Admin") {
					return true
				}
			}
		case string:
			if r == "Super Admin" || r == "Administrador" || r == "Admin" {
				return true
			}
		}
	}

	// 2. Verificar permisos en Claims (Token)
	if permsRaw, ok := claims["permissions"]; ok {
		switch p := permsRaw.(type) {
		case []interface{}:
			for _, perm := range p {
				if s, ok := perm.(string); ok && s == "admin_biblioteca" {
					return true
				}
			}
		case string:
			if p == "admin_biblioteca" {
				return true
			}
		}
	}

	// 3. Consulta de respaldo rápida a la Base de Datos Local
	var usuarioID uint
	if sub, ok := claims["sub"]; ok {
		if idFloat, ok := sub.(float64); ok {
			usuarioID = uint(idFloat)
		} else if idStr, ok := sub.(string); ok {
			if parsed, err := strconv.ParseUint(idStr, 10, 32); err == nil {
				usuarioID = uint(parsed)
			}
		}
	}

	if usuarioID == 0 {
		return false
	}

	var userLocal models.Usuario
	// Hacemos un select exclusivo de Roles y Permissions para evitar overhead
	if err := db.DB.Select("roles, permissions").First(&userLocal, usuarioID).Error; err != nil {
		return false
	}

	// Verificar Roles en la BD
	if userLocal.Roles != nil {
		var roles []string
		if err := json.Unmarshal([]byte(*userLocal.Roles), &roles); err == nil {
			for _, r := range roles {
				if r == "Super Admin" || r == "Administrador" || r == "Admin" {
					return true
				}
			}
		} else if strings.Contains(*userLocal.Roles, "Super Admin") {
			return true
		}
	}

	// Verificar Permisos en la BD
	if userLocal.Permissions != nil {
		var perms []string
		if err := json.Unmarshal([]byte(*userLocal.Permissions), &perms); err == nil {
			for _, p := range perms {
				if p == "admin_biblioteca" {
					return true
				}
			}
		} else if strings.Contains(*userLocal.Permissions, "admin_biblioteca") {
			return true
		}
	}

	return false
}

// hasPermissionOrAdmin verifica si el usuario es Admin o posee un permiso específico (como reportes_normativas)
func hasPermissionOrAdmin(c *fiber.Ctx, perm string) bool {
	if isUserAdmin(c) {
		return true
	}
	claims, ok := c.Locals("userClaims").(jwt.MapClaims)
	if !ok {
		return false
	}
	if permsRaw, ok := claims["permissions"]; ok {
		switch p := permsRaw.(type) {
		case []interface{}:
			for _, item := range p {
				if s, ok := item.(string); ok && s == perm {
					return true
				}
			}
		case string:
			if p == perm {
				return true
			}
		}
	}
	return false
}

// getUsuarioPuestoID obtiene el ID de puesto del usuario autenticado
func getUsuarioPuestoID(c *fiber.Ctx) uint {
	claims, ok := c.Locals("userClaims").(jwt.MapClaims)
	if !ok {
		return 0
	}

	var usuarioID uint
	if sub, ok := claims["sub"]; ok {
		if idFloat, ok := sub.(float64); ok {
			usuarioID = uint(idFloat)
		} else if idStr, ok := sub.(string); ok {
			if parsed, err := strconv.ParseUint(idStr, 10, 32); err == nil {
				usuarioID = uint(parsed)
			}
		}
	}

	if usuarioID == 0 {
		return 0
	}

	var userLocal models.Usuario
	if err := db.DB.Select("id_puesto").First(&userLocal, usuarioID).Error; err != nil {
		return 0
	}

	if userLocal.IDPuesto != nil {
		return *userLocal.IDPuesto
	}

	return 0
}

// === CONTROLADORES DE CATEGORÍAS (ADMIN) ===

func GetAdminCategorias(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	var categorias []models.ManualCategoria
	// Precargamos la jerarquía completa de 3 niveles: Categorias (Gavetas) -> Subcategorias (Portafolios) -> Carpetas -> Documentos -> Relaciones
	err := db.DB.Preload("Subcategorias", func(db *gorm.DB) *gorm.DB {
		return db.Order("nombre ASC")
	}).Preload("Subcategorias.Carpetas", func(db *gorm.DB) *gorm.DB {
		return db.Order("nombre ASC")
	}).Preload("Subcategorias.Carpetas.Documentos", func(db *gorm.DB) *gorm.DB {
		return db.Preload("PuestosAutorizados").Order("titulo ASC")
	}).Preload("Subcategorias.Carpetas.Documentos.Actualizaciones", func(db *gorm.DB) *gorm.DB {
		return db.Order("id ASC")
	}).Order("nombre ASC").Find(&categorias).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al obtener categorías"})
	}

	return c.JSON(categorias)
}

func CreateCategoria(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	categoria := new(models.ManualCategoria)
	if err := c.BodyParser(categoria); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	if strings.TrimSpace(categoria.Nombre) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El nombre es obligatorio"})
	}

	if err := db.DB.Create(categoria).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al crear la categoría"})
	}

	return c.Status(fiber.StatusCreated).JSON(categoria)
}

func UpdateCategoria(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	id := c.Params("id")
	categoria := new(models.ManualCategoria)

	if err := db.DB.First(categoria, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Categoría no encontrada"})
	}

	if err := c.BodyParser(categoria); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	if strings.TrimSpace(categoria.Nombre) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El nombre es obligatorio"})
	}

	if err := db.DB.Save(categoria).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar la categoría"})
	}

	return c.JSON(categoria)
}

// === CONTROLADORES DE SUBCATEGORÍAS (ADMIN) ===

func CreateSubcategoria(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	subcategoria := new(models.ManualSubcategoria)
	if err := c.BodyParser(subcategoria); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	if subcategoria.ManualCategoriaID == 0 || strings.TrimSpace(subcategoria.Nombre) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "CategoríaID y Nombre son obligatorios"})
	}

	if err := db.DB.Create(subcategoria).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al crear la subcategoría"})
	}

	return c.Status(fiber.StatusCreated).JSON(subcategoria)
}

func UpdateSubcategoria(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	id := c.Params("id")
	subcategoria := new(models.ManualSubcategoria)

	if err := db.DB.First(subcategoria, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Subcategoría no encontrada"})
	}

	if err := c.BodyParser(subcategoria); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	if subcategoria.ManualCategoriaID == 0 || strings.TrimSpace(subcategoria.Nombre) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "CategoríaID y Nombre son obligatorios"})
	}

	if err := db.DB.Save(subcategoria).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar la subcategoría"})
	}

	return c.JSON(subcategoria)
}

// === CONTROLADORES DE CARPETAS (ADMIN) ===

func CreateCarpeta(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	carpeta := new(models.ManualCarpeta)
	if err := c.BodyParser(carpeta); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	if carpeta.ManualSubcategoriaID == 0 || strings.TrimSpace(carpeta.Nombre) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "SubcategoriaID (Portafolio) y Nombre son obligatorios"})
	}

	if err := db.DB.Create(carpeta).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al crear la carpeta"})
	}

	return c.Status(fiber.StatusCreated).JSON(carpeta)
}

func UpdateCarpeta(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	id := c.Params("id")
	carpeta := new(models.ManualCarpeta)

	if err := db.DB.First(carpeta, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Carpeta no encontrada"})
	}

	if err := c.BodyParser(carpeta); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	if carpeta.ManualSubcategoriaID == 0 || strings.TrimSpace(carpeta.Nombre) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "SubcategoriaID (Portafolio) y Nombre son obligatorios"})
	}

	if err := db.DB.Save(carpeta).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar la carpeta"})
	}

	return c.JSON(carpeta)
}

// === CONTROLADORES DE DOCUMENTOS (ADMIN & READERS) ===

func SubirManual(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	carpetaIDStr := c.FormValue("manual_carpeta_id")
	titulo := c.FormValue("titulo")

	if carpetaIDStr == "" || strings.TrimSpace(titulo) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Faltan parámetros obligatorios (manual_carpeta_id, titulo)"})
	}

	carpetaID, err := strconv.ParseUint(carpetaIDStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de carpeta inválido"})
	}

	file, err := c.FormFile("documento")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No se recibió ningún archivo PDF válido"})
	}

	// Obtener ID de usuario
	claims, _ := c.Locals("userClaims").(jwt.MapClaims)
	var usuarioID uint = 1
	if sub, ok := claims["sub"]; ok {
		if idFloat, ok := sub.(float64); ok {
			usuarioID = uint(idFloat)
		} else if idStr, ok := sub.(string); ok {
			if parsed, err := strconv.ParseUint(idStr, 10, 32); err == nil {
				usuarioID = uint(parsed)
			}
		}
	}

	// Crear archivo temporal local para procesar con pdfcpu
	tempFile, err := os.CreateTemp("", "manual_gcs_*.pdf")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al crear archivo temporal"})
	}
	tempFilePath := tempFile.Name()
	defer os.Remove(tempFilePath)
	tempFile.Close()

	if err := c.SaveFile(file, tempFilePath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al guardar archivo localmente"})
	}

	totalPaginas, err := api.PageCountFile(tempFilePath)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El PDF subido no es válido o está corrupto"})
	}

	// Comprimir PDF antes de subirlo con Ghostscript
	tempOutPath := tempFilePath + ".compressed.pdf"
	if errComp := utils.CompressPDF(c.UserContext(), tempFilePath, tempOutPath); errComp == nil {
		tempFilePath = tempOutPath
		defer os.Remove(tempOutPath)
	} else {
		fmt.Printf("[WARN] No se pudo comprimir el PDF con Ghostscript %s: %v\n", tempFilePath, errComp)
	}

	// Abrir archivo temporal
	fToUpload, err := os.Open(tempFilePath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al abrir temporal para subir"})
	}
	defer fToUpload.Close()

	// Guardar en Google Cloud Storage
	timestamp := time.Now().UnixNano()
	fileNameCleaned := strings.ToLower(strings.ReplaceAll(titulo, " ", "_"))
	gcsObjectName := fmt.Sprintf("App_Manuales/carpeta_%d/%d_%s.pdf", carpetaID, timestamp, fileNameCleaned)

	if err := gcs.SubirArchivo(c.UserContext(), gcsObjectName, fToUpload); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al subir manual a GCS", "detalle": err.Error()})
	}

	numeroActa := c.FormValue("numero_acta")
	fechaAprobacionStr := c.FormValue("fecha_aprobacion")
	fechaVigenciaStr := c.FormValue("fecha_vigencia")

	var fechaAprobacion *time.Time
	if fechaAprobacionStr != "" {
		if parsedDate, err := time.Parse("2006-01-02", fechaAprobacionStr); err == nil {
			fechaAprobacion = &parsedDate
		}
	}

	var fechaVigencia *time.Time
	if fechaVigenciaStr != "" {
		if parsedDate, err := time.Parse("2006-01-02", fechaVigenciaStr); err == nil {
			fechaVigencia = &parsedDate
		}
	}

	carpetaIDVal := uint(carpetaID)
	// Crear el registro del manual
	manual := models.ManualDocumento{
		ManualCarpetaID: &carpetaIDVal,
		Titulo:          titulo,
		FilePath:        gcsObjectName,
		TotalPaginas:    totalPaginas,
		UsuarioID:       usuarioID,
		NumeroActa:      numeroActa,
		FechaAprobacion: fechaAprobacion,
		FechaVigencia:   fechaVigencia,
	}

	if err := db.DB.Create(&manual).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Error al guardar manual en base de datos",
			"detalle": err.Error(),
		})
	}

	// Procesar los puestos autorizados
	puestosStr := c.FormValue("puestos_autorizados") // Comma-separated list de IDs de puestos: "1,2,5"
	if puestosStr != "" {
		puestosIDs := strings.Split(puestosStr, ",")
		var puestos []models.Puesto
		for _, pIDStr := range puestosIDs {
			pIDStr = strings.TrimSpace(pIDStr)
			if pID, err := strconv.ParseUint(pIDStr, 10, 32); err == nil {
				var puesto models.Puesto
				if db.DB.First(&puesto, pID).Error == nil {
					puestos = append(puestos, puesto)
				}
			}
		}
		if len(puestos) > 0 {
			db.DB.Model(&manual).Association("PuestosAutorizados").Replace(puestos)
		}
	}

	// Recargar datos
	db.DB.Preload("Carpeta").Preload("PuestosAutorizados").First(&manual, manual.ID)

	return c.Status(fiber.StatusCreated).JSON(manual)
}

func UpdateManual(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	id := c.Params("id")
	manual := new(models.ManualDocumento)

	if err := db.DB.Preload("PuestosAutorizados").First(manual, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Manual no encontrado"})
	}

	titulo := c.FormValue("titulo")
	carpetaIDStr := c.FormValue("manual_carpeta_id")

	if titulo != "" {
		manual.Titulo = titulo
	}
	if carpetaIDStr != "" {
		if carpID, err := strconv.ParseUint(carpetaIDStr, 10, 32); err == nil {
			carpIDVal := uint(carpID)
			manual.ManualCarpetaID = &carpIDVal
		}
	}

	// Actualizar nuevos campos
	manual.NumeroActa = c.FormValue("numero_acta")

	fechaAprobacionStr := c.FormValue("fecha_aprobacion")
	if fechaAprobacionStr != "" {
		if parsedDate, err := time.Parse("2006-01-02", fechaAprobacionStr); err == nil {
			manual.FechaAprobacion = &parsedDate
		}
	} else {
		manual.FechaAprobacion = nil
	}

	fechaVigenciaStr := c.FormValue("fecha_vigencia")
	if fechaVigenciaStr != "" {
		if parsedDate, err := time.Parse("2006-01-02", fechaVigenciaStr); err == nil {
			manual.FechaVigencia = &parsedDate
		}
	} else {
		manual.FechaVigencia = nil
	}

	// Actualizar archivo físico si se proporciona
	file, err := c.FormFile("documento")
	if err == nil {
		// Crear archivo temporal local
		tempFile, err := os.CreateTemp("", "manual_gcs_update_*.pdf")
		if err == nil {
			tempFilePath := tempFile.Name()
			defer os.Remove(tempFilePath)
			tempFile.Close()

			if c.SaveFile(file, tempFilePath) == nil {
				totalPaginas, countErr := api.PageCountFile(tempFilePath)
				if countErr == nil {
					// Comprimir PDF antes de subirlo con Ghostscript
					tempOutPath := tempFilePath + ".compressed.pdf"
					if errComp := utils.CompressPDF(c.UserContext(), tempFilePath, tempOutPath); errComp == nil {
						tempFilePath = tempOutPath
						defer os.Remove(tempOutPath)
					} else {
						fmt.Printf("[WARN] No se pudo comprimir el PDF con Ghostscript %s: %v\n", tempFilePath, errComp)
					}
				}
				fToUpload, openErr := os.Open(tempFilePath)
				if countErr == nil && openErr == nil {
					defer fToUpload.Close()
					// Subir a GCS (Sobreescribimos la ruta existente)
					if gcs.SubirArchivo(c.UserContext(), manual.FilePath, fToUpload) == nil {
						manual.TotalPaginas = totalPaginas
					}
				}
			}
		}
	}

	if err := db.DB.Save(manual).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar metadatos del manual"})
	}

	// Actualizar puestos autorizados
	puestosStr := c.FormValue("puestos_autorizados")
	if puestosStr != "" {
		puestosIDs := strings.Split(puestosStr, ",")
		var puestos []models.Puesto
		for _, pIDStr := range puestosIDs {
			pIDStr = strings.TrimSpace(pIDStr)
			if pID, err := strconv.ParseUint(pIDStr, 10, 32); err == nil {
				var puesto models.Puesto
				if db.DB.First(&puesto, pID).Error == nil {
					puestos = append(puestos, puesto)
				}
			}
		}
		db.DB.Model(manual).Association("PuestosAutorizados").Replace(puestos)
	} else {
		// Limpiar todos si viene explícitamente vacío
		db.DB.Model(manual).Association("PuestosAutorizados").Clear()
	}

	db.DB.Preload("Carpeta").Preload("PuestosAutorizados").First(manual, manual.ID)

	return c.JSON(manual)
}

func DeleteManual(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	id := c.Params("id")
	var manual models.ManualDocumento

	if err := db.DB.First(&manual, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Manual no encontrado"})
	}

	// 1. Eliminar archivo físico de GCS
	if err := gcs.EliminarArchivo(c.UserContext(), manual.FilePath); err != nil {
		// Logueamos el error pero permitimos borrar de la base de datos para no dejar registros corruptos huérfanos
		fmt.Printf("[WARN] Falló al eliminar manual en GCS (%s): %v\n", manual.FilePath, err)
	}

	// 2. Limpiar relación muchos a muchos
	db.DB.Model(&manual).Association("PuestosAutorizados").Clear()

	// 3. Eliminar registro de base de datos
	if err := db.DB.Delete(&manual).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al eliminar registro de manual"})
	}

	return c.JSON(fiber.Map{"status": "ok", "message": "Manual eliminado correctamente"})
}

// === VISTA DE LECTURA (BIBLIOTECA - RENDIMIENTO BALA) ===

func GetBibliotecaManuales(c *fiber.Ctx) error {
	isAdmin := isUserAdmin(c)
	puestoID := getUsuarioPuestoID(c)

	var categorias []models.ManualCategoria

	// Consulta optimizada evitando N+1.
	// Si es administrador general, tiene visualización total de todos los manuales activos o inactivos.
	// Si es un lector estándar, solo pre-cargamos los manuales donde su puesto está autorizado.
	query := db.DB.Preload("Subcategorias", "estado = ?", true).
		Preload("Subcategorias.Carpetas", "estado = ?", true)

	if isAdmin {
		// Admin ve todo
		err := query.Preload("Subcategorias.Carpetas.Documentos", func(db *gorm.DB) *gorm.DB {
			return db.Preload("PuestosAutorizados").Order("titulo ASC")
		}).Preload("Subcategorias.Carpetas.Documentos.Actualizaciones", func(db *gorm.DB) *gorm.DB {
			return db.Order("id ASC")
		}).Order("nombre ASC").Find(&categorias).Error

		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al recuperar biblioteca completa"})
		}
	} else {
		// Lector regular - Restringido por Puesto
		// Precargamos los documentos autorizados con un query join ágil
		err := query.Preload("Subcategorias.Carpetas.Documentos", func(db *gorm.DB) *gorm.DB {
			return db.Joins("JOIN manual_documento_puestos mdp ON mdp.manual_documento_id = manual_documentos.id").
				Where("mdp.puesto_id = ?", puestoID).
				Order("titulo ASC")
		}).Preload("Subcategorias.Carpetas.Documentos.Actualizaciones", func(db *gorm.DB) *gorm.DB {
			return db.Order("id ASC")
		}).Where("estado = ?", true).Order("nombre ASC").Find(&categorias).Error

		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al procesar la biblioteca de manuales"})
		}

		// --- OPTIMIZACIÓN EXTREMA ---
		// Limpiamos la jerarquía para no enviar carpetas ni categorías vacías al cliente
		var categoriasFiltradas []models.ManualCategoria
		for _, cat := range categorias {
			var subcategoriasFiltradas []models.ManualSubcategoria
			for _, sub := range cat.Subcategorias {
				var carpetasFiltradas []models.ManualCarpeta
				for _, carp := range sub.Carpetas {
					if len(carp.Documentos) > 0 {
						carpetasFiltradas = append(carpetasFiltradas, carp)
					}
				}
				if len(carpetasFiltradas) > 0 {
					sub.Carpetas = carpetasFiltradas
					subcategoriasFiltradas = append(subcategoriasFiltradas, sub)
				}
			}
			if len(subcategoriasFiltradas) > 0 {
				cat.Subcategorias = subcategoriasFiltradas
				categoriasFiltradas = append(categoriasFiltradas, cat)
			}
		}
		categorias = categoriasFiltradas
	}

	return c.JSON(categorias)
}

func GenerarURLManual(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de manual inválido"})
	}

	var manual models.ManualDocumento
	if err := db.DB.Preload("PuestosAutorizados").First(&manual, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Manual no encontrado"})
	}

	// Validar permisos
	canView := hasPermissionOrAdmin(c, "reportes_normativas")
	if !canView {
		// Validar si el puesto del lector regular está autorizado
		puestoID := getUsuarioPuestoID(c)
		authorized := false
		for _, p := range manual.PuestosAutorizados {
			if p.ID == puestoID {
				authorized = true
				break
			}
		}
		if !authorized {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes autorización de lectura para este manual"})
		}
	}

	// Generar enlace seguro en GCS por 1 minuto
	url, err := gcs.GenerarURLFirmada(manual.FilePath, 1*time.Minute)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al generar enlace seguro de visualización"})
	}

	return c.JSON(fiber.Map{"url": url})
}

func SubirActualizacion(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	manualIDStr := c.Params("id")
	manualID, err := strconv.ParseUint(manualIDStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de manual inválido"})
	}

	var manual models.ManualDocumento
	if err := db.DB.First(&manual, manualID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Manual no encontrado"})
	}

	numeroActa := c.FormValue("numero_acta")
	fechaAprobacionStr := c.FormValue("fecha_aprobacion")
	fechaVigenciaStr := c.FormValue("fecha_vigencia")
	descripcion := c.FormValue("descripcion")

	if strings.TrimSpace(numeroActa) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El número de acta es obligatorio"})
	}

	// 1. Recibir ambos archivos
	fileChanges, errChanges := c.FormFile("documento") // Hojas que cambiaron
	if errChanges != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No se recibió el archivo PDF con las hojas de cambio"})
	}

	fileOriginal, errOriginal := c.FormFile("documento_original") // Manual completo consolidado
	if errOriginal != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No se recibió el archivo PDF original completo actualizado"})
	}

	// Obtener ID de usuario
	claims, _ := c.Locals("userClaims").(jwt.MapClaims)
	var usuarioID uint = 1
	if sub, ok := claims["sub"]; ok {
		if idFloat, ok := sub.(float64); ok {
			usuarioID = uint(idFloat)
		} else if idStr, ok := sub.(string); ok {
			if parsed, err := strconv.ParseUint(idStr, 10, 32); err == nil {
				usuarioID = uint(parsed)
			}
		}
	}

	// Parsear fechas
	var fechaAprobacion *time.Time
	if fechaAprobacionStr != "" {
		if parsedDate, err := time.Parse("2006-01-02", fechaAprobacionStr); err == nil {
			fechaAprobacion = &parsedDate
		}
	}

	var fechaVigencia *time.Time
	if fechaVigenciaStr != "" {
		if parsedDate, err := time.Parse("2006-01-02", fechaVigenciaStr); err == nil {
			fechaVigencia = &parsedDate
		}
	}

	// ==================== PROCESAR MANUAL COMPLETO ORIGINAL ====================
	tempFileOrig, err := os.CreateTemp("", "manual_full_gcs_*.pdf")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al crear archivo temporal para manual completo"})
	}
	tempFilePathOrig := tempFileOrig.Name()
	defer os.Remove(tempFilePathOrig)
	tempFileOrig.Close()

	if err := c.SaveFile(fileOriginal, tempFilePathOrig); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al guardar manual completo localmente"})
	}

	totalPaginasOriginal, err := api.PageCountFile(tempFilePathOrig)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El PDF del manual completo no es válido o está corrupto"})
	}

	// Comprimir PDF antes de subirlo con Ghostscript
	tempOutPathOrig := tempFilePathOrig + ".compressed.pdf"
	if errComp := utils.CompressPDF(c.UserContext(), tempFilePathOrig, tempOutPathOrig); errComp == nil {
		tempFilePathOrig = tempOutPathOrig
		defer os.Remove(tempOutPathOrig)
	} else {
		fmt.Printf("[WARN] No se pudo comprimir el PDF original con Ghostscript %s: %v\n", tempFilePathOrig, errComp)
	}

	fOriginalToUpload, err := os.Open(tempFilePathOrig)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al abrir manual completo para subir"})
	}
	defer fOriginalToUpload.Close()

	// Eliminar el archivo físico actual en GCS
	if manual.FilePath != "" {
		if errDel := gcs.EliminarArchivo(c.UserContext(), manual.FilePath); errDel != nil {
			fmt.Printf("[WARN] Falló al eliminar manual completo anterior en GCS (%s): %v\n", manual.FilePath, errDel)
		}
	}

	// Subir nuevo manual completo consolidado
	timestamp := time.Now().UnixNano()
	fileNameCleaned := strings.ToLower(strings.ReplaceAll(manual.Titulo, " ", "_"))
	gcsObjectNameOriginal := fmt.Sprintf("App_Manuales/carpeta_%d/%d_%s.pdf", manual.ManualCarpetaID, timestamp, fileNameCleaned)

	if err := gcs.SubirArchivo(c.UserContext(), gcsObjectNameOriginal, fOriginalToUpload); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al subir el nuevo manual completo a GCS", "detalle": err.Error()})
	}

	// Actualizar registro en base de datos del manual principal
	manual.FilePath = gcsObjectNameOriginal
	manual.TotalPaginas = totalPaginasOriginal
	manual.UltimaActualizacion = time.Now()
	manual.NumeroActa = numeroActa
	if fechaVigencia != nil {
		manual.FechaVigencia = fechaVigencia
	}

	if err := db.DB.Save(&manual).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar manual principal en base de datos"})
	}

	// ==================== PROCESAR HOJAS DE CAMBIO (ANEXO) ====================
	// 1. Buscar todas las actualizaciones previas de este manual
	var prevUpdates []models.ManualActualizacion
	if err := db.DB.Where("manual_documento_id = ?", manual.ID).Find(&prevUpdates).Error; err == nil {
		for _, prev := range prevUpdates {
			if prev.FilePath != "" {
				// Eliminar archivo físico de GCS
				if errDel := gcs.EliminarArchivo(c.UserContext(), prev.FilePath); errDel != nil {
					fmt.Printf("[WARN] Falló al eliminar hojas de cambio obsoletas en GCS (%s): %v\n", prev.FilePath, errDel)
				}
				// Limpiar ruta en DB
				db.DB.Model(&prev).Update("file_path", "")
			}
		}
	}

	// 2. Procesar las nuevas hojas de cambio
	tempFileChanges, err := os.CreateTemp("", "manual_changes_gcs_*.pdf")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al crear archivo temporal para hojas de cambio"})
	}
	tempFilePathChanges := tempFileChanges.Name()
	defer os.Remove(tempFilePathChanges)
	tempFileChanges.Close()

	if err := c.SaveFile(fileChanges, tempFilePathChanges); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al guardar hojas de cambio localmente"})
	}

	totalPaginasChanges, err := api.PageCountFile(tempFilePathChanges)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El PDF de hojas de cambio no es válido o está corrupto"})
	}

	// Comprimir PDF antes de subirlo con Ghostscript
	tempOutPathChanges := tempFilePathChanges + ".compressed.pdf"
	if errComp := utils.CompressPDF(c.UserContext(), tempFilePathChanges, tempOutPathChanges); errComp == nil {
		tempFilePathChanges = tempOutPathChanges
		defer os.Remove(tempOutPathChanges)
	} else {
		fmt.Printf("[WARN] No se pudo comprimir el PDF de cambios con Ghostscript %s: %v\n", tempFilePathChanges, errComp)
	}

	fChangesToUpload, err := os.Open(tempFilePathChanges)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al abrir hojas de cambio para subir"})
	}
	defer fChangesToUpload.Close()

	// Subir archivo a GCS
	gcsObjectNameChanges := fmt.Sprintf("App_Manuales/updates/manual_%d/%d_changes.pdf", manual.ID, timestamp)
	if err := gcs.SubirArchivo(c.UserContext(), gcsObjectNameChanges, fChangesToUpload); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al subir hojas de cambio a GCS", "detalle": err.Error()})
	}

	// 3. Crear el nuevo registro en la base de datos
	actualizacion := models.ManualActualizacion{
		ManualDocumentoID: uint(manual.ID),
		NumeroActa:        numeroActa,
		FechaAprobacion:   fechaAprobacion,
		FechaVigencia:     fechaVigencia,
		Descripcion:       descripcion,
		FilePath:          gcsObjectNameChanges,
		TotalPaginas:      totalPaginasChanges,
		UsuarioID:         usuarioID,
	}

	if err := db.DB.Create(&actualizacion).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al guardar actualización en base de datos"})
	}

	// Recargar e incluir Usuario
	db.DB.Preload("Usuario").First(&actualizacion, actualizacion.ID)

	return c.Status(fiber.StatusCreated).JSON(actualizacion)
}
func DeleteActualizacion(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	id := c.Params("id")
	var actualizacion models.ManualActualizacion

	if err := db.DB.First(&actualizacion, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Hoja de actualización no encontrada"})
	}

	// Eliminar de GCS
	if err := gcs.EliminarArchivo(c.UserContext(), actualizacion.FilePath); err != nil {
		fmt.Printf("[WARN] Falló al eliminar actualización en GCS (%s): %v\n", actualizacion.FilePath, err)
	}

	// Eliminar de base de datos
	if err := db.DB.Delete(&actualizacion).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al eliminar registro de actualización"})
	}

	return c.JSON(fiber.Map{"status": "ok", "message": "Hoja de actualización eliminada correctamente"})
}

func GenerarURLActualizacion(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de actualización inválido"})
	}

	var actualizacion models.ManualActualizacion
	if err := db.DB.First(&actualizacion, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Hoja de actualización no encontrada"})
	}

	// Validar permisos del manual padre
	var manual models.ManualDocumento
	if err := db.DB.Preload("PuestosAutorizados").First(&manual, actualizacion.ManualDocumentoID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Manual asociado no encontrado"})
	}

	canView := hasPermissionOrAdmin(c, "reportes_normativas")
	if !canView {
		puestoID := getUsuarioPuestoID(c)
		authorized := false
		for _, p := range manual.PuestosAutorizados {
			if p.ID == puestoID {
				authorized = true
				break
			}
		}
		if !authorized {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes autorización de lectura para este manual"})
		}
	}

	// Generar enlace seguro en GCS por 1 minuto
	url, err := gcs.GenerarURLFirmada(actualizacion.FilePath, 1*time.Minute)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al generar enlace seguro de visualización"})
	}

	return c.JSON(fiber.Map{"url": url})
}

func GetAdminManuales(c *fiber.Ctx) error {
	if !isUserAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	pageStr := c.Query("page", "1")
	limitStr := c.Query("limit", "10")
	search := c.Query("search", "")

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		limit = 10
	}

	offset := (page - 1) * limit

	var total int64
	var documentos []models.ManualDocumento

	query := db.DB.Model(&models.ManualDocumento{})

	if search != "" {
		query = query.Where("titulo LIKE ?", "%"+search+"%")
	}

	if err := query.Count(&total).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al contar manuales"})
	}

	err = query.
		Preload("PuestosAutorizados").
		Preload("Actualizaciones").
		Preload("Carpeta.Subcategoria.Categoria").
		Order("id DESC").
		Limit(limit).
		Offset(offset).
		Find(&documentos).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al obtener manuales"})
	}

	return c.JSON(fiber.Map{
		"documentos": documentos,
		"total":      total,
		"page":       page,
		"limit":      limit,
	})
}

// === MÓDULO DE REPORTERÍA DE NORMATIVAS ===

// GetReporteNormativasFiltros obtiene la taxonomía completa (Gavetas, Portafolios, Carpetas y Puestos) para alimentar los filtros del reporte
func GetReporteNormativasFiltros(c *fiber.Ctx) error {
	var categorias []models.ManualCategoria
	err := db.DB.Preload("Subcategorias", func(db *gorm.DB) *gorm.DB {
		return db.Order("nombre ASC")
	}).Preload("Subcategorias.Carpetas", func(db *gorm.DB) *gorm.DB {
		return db.Order("nombre ASC")
	}).Order("nombre ASC").Find(&categorias).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al obtener filtros de categorías"})
	}

	var puestos []models.Puesto
	if err := db.DB.Order("nombre ASC").Find(&puestos).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al obtener puestos"})
	}

	return c.JSON(fiber.Map{
		"categorias": categorias,
		"puestos":    puestos,
	})
}

// GetReporteNormativas consulta las normativas con soporte para filtros por gaveta, área/portafolio, tipo/carpeta y búsqueda
func GetReporteNormativas(c *fiber.Ctx) error {
	search := strings.TrimSpace(c.Query("search", ""))
	categoriaID := strings.TrimSpace(c.Query("categoria_id", ""))
	subcategoriaID := strings.TrimSpace(c.Query("subcategoria_id", ""))
	carpetaID := strings.TrimSpace(c.Query("carpeta_id", ""))
	startDate := strings.TrimSpace(c.Query("start_date", ""))
	endDate := strings.TrimSpace(c.Query("end_date", ""))

	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.Query("limit", "15"))
	if limit < 1 {
		limit = 15
	}
	offset := (page - 1) * limit

	query := db.DB.Model(&models.ManualDocumento{})

	if categoriaID != "" || subcategoriaID != "" {
		query = query.Joins("JOIN manual_carpetas mc ON mc.id = manual_documentos.manual_carpeta_id")
		if categoriaID != "" {
			query = query.Joins("JOIN manual_subcategorias ms ON ms.id = mc.manual_subcategoria_id").
				Where("ms.manual_categoria_id = ?", categoriaID)
		}
		if subcategoriaID != "" {
			query = query.Where("mc.manual_subcategoria_id = ?", subcategoriaID)
		}
	}

	if carpetaID != "" {
		query = query.Where("manual_documentos.manual_carpeta_id = ?", carpetaID)
	}

	if search != "" {
		s := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(manual_documentos.titulo) LIKE ? OR LOWER(manual_documentos.numero_acta) LIKE ?", s, s)
	}

	if startDate != "" && endDate != "" {
		query = query.Where("(manual_documentos.fecha_aprobacion BETWEEN ? AND ?) OR (manual_documentos.fecha_creacion BETWEEN ? AND ?)",
			startDate+" 00:00:00", endDate+" 23:59:59", startDate+" 00:00:00", endDate+" 23:59:59")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al contar normativas"})
	}

	var documentos []models.ManualDocumento
	err := query.
		Preload("PuestosAutorizados").
		Preload("Actualizaciones", func(db *gorm.DB) *gorm.DB {
			return db.Order("id ASC")
		}).
		Preload("Carpeta.Subcategoria.Categoria").
		Preload("Usuario").
		Order("manual_documentos.id DESC").
		Limit(limit).
		Offset(offset).
		Find(&documentos).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al consultar normativas"})
	}

	return c.JSON(fiber.Map{
		"documentos": documentos,
		"total":      total,
		"page":       page,
		"limit":      limit,
	})
}

// ExportarReporteNormativasCSV exporta a CSV los documentos normativos vigentes con las 3 columnas de versiones y responsables
func ExportarReporteNormativasCSV(c *fiber.Ctx) error {
	search := strings.TrimSpace(c.Query("search", ""))
	categoriaID := strings.TrimSpace(c.Query("categoria_id", ""))
	subcategoriaID := strings.TrimSpace(c.Query("subcategoria_id", ""))
	carpetaID := strings.TrimSpace(c.Query("carpeta_id", ""))
	startDate := strings.TrimSpace(c.Query("start_date", ""))
	endDate := strings.TrimSpace(c.Query("end_date", ""))

	query := db.DB.Model(&models.ManualDocumento{})

	if categoriaID != "" || subcategoriaID != "" {
		query = query.Joins("JOIN manual_carpetas mc ON mc.id = manual_documentos.manual_carpeta_id")
		if categoriaID != "" {
			query = query.Joins("JOIN manual_subcategorias ms ON ms.id = mc.manual_subcategoria_id").
				Where("ms.manual_categoria_id = ?", categoriaID)
		}
		if subcategoriaID != "" {
			query = query.Where("mc.manual_subcategoria_id = ?", subcategoriaID)
		}
	}

	if carpetaID != "" {
		query = query.Where("manual_documentos.manual_carpeta_id = ?", carpetaID)
	}

	if search != "" {
		s := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(manual_documentos.titulo) LIKE ? OR LOWER(manual_documentos.numero_acta) LIKE ?", s, s)
	}

	if startDate != "" && endDate != "" {
		query = query.Where("(manual_documentos.fecha_aprobacion BETWEEN ? AND ?) OR (manual_documentos.fecha_creacion BETWEEN ? AND ?)",
			startDate+" 00:00:00", endDate+" 23:59:59", startDate+" 00:00:00", endDate+" 23:59:59")
	}

	var documentos []models.ManualDocumento
	err := query.
		Preload("PuestosAutorizados").
		Preload("Actualizaciones", func(db *gorm.DB) *gorm.DB {
			return db.Order("id ASC")
		}).
		Preload("Carpeta.Subcategoria.Categoria").
		Preload("Usuario").
		Order("manual_documentos.id ASC").
		Find(&documentos).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al consultar normativas para exportar"})
	}

	c.Set("Content-Type", "text/csv; charset=utf-8")
	c.Set("Content-Disposition", "attachment; filename=reporte_normativas_vigentes.csv")

	// Prepend UTF-8 BOM so Excel opens accents and special characters without encoding glitches
	c.Response().BodyWriter().Write([]byte{0xEF, 0xBB, 0xBF})

	writer := csv.NewWriter(c.Response().BodyWriter())
	defer writer.Flush()

	// Encabezados del reporte
	writer.Write([]string{
		"ID",
		"Título de la Normativa",
		"Estado",
		"Tipo Documental",
		"Portafolio / Área",
		"Gaveta",
		"Páginas Físicas",
		"Responsables / Puestos con Acceso",
		"No. Acta Aprobación",
		"Fecha Aprobación",
		"Descripción del Cambio",
		"Total de Actualizaciones",
		"Fecha de Registro",
		"Última Modificación",
	})

	for _, doc := range documentos {
		tipoDoc := "No especificado"
		area := "No asignada"
		gaveta := "No asignada"

		if doc.Carpeta.Nombre != "" {
			tipoDoc = doc.Carpeta.Nombre
		}
		if doc.Carpeta.Subcategoria.Nombre != "" {
			area = doc.Carpeta.Subcategoria.Nombre
		}
		if doc.Carpeta.Subcategoria.Categoria.Nombre != "" {
			gaveta = doc.Carpeta.Subcategoria.Categoria.Nombre
		}

		// Responsables / Puestos con acceso
		puestosList := make([]string, 0, len(doc.PuestosAutorizados))
		for _, p := range doc.PuestosAutorizados {
			puestosList = append(puestosList, p.Nombre)
		}
		puestosStr := "Acceso general (Todos los puestos)"
		if len(puestosList) > 0 {
			puestosStr = strings.Join(puestosList, " | ")
		}

		// Control de Versiones en 3 Columnas:
		// 1. No. Acta Aprobación
		// 2. Fecha Aprobación
		// 3. Descripción del Cambio
		actasList := []string{}
		fechasList := []string{}
		descList := []string{}

		// Entrada inicial
		actaBase := doc.NumeroActa
		if strings.TrimSpace(actaBase) == "" {
			actaBase = "Sin acta registrada"
		}
		actasList = append(actasList, fmt.Sprintf("[Inicial] %s", actaBase))

		fechaBase := "No especificada"
		if doc.FechaAprobacion != nil {
			fechaBase = doc.FechaAprobacion.Format("2006-01-02")
		}
		fechasList = append(fechasList, fmt.Sprintf("[Inicial] %s", fechaBase))
		descList = append(descList, "[Inicial] Aprobación y emisión inicial")

		// Actualizaciones posteriores
		for i, act := range doc.Actualizaciones {
			vNum := i + 1
			actaAct := act.NumeroActa
			if strings.TrimSpace(actaAct) == "" {
				actaAct = fmt.Sprintf("Acta Act. %d", vNum)
			}
			actasList = append(actasList, fmt.Sprintf("[Act. %d] %s", vNum, actaAct))

			fAct := "No especificada"
			if act.FechaAprobacion != nil {
				fAct = act.FechaAprobacion.Format("2006-01-02")
			}
			fechasList = append(fechasList, fmt.Sprintf("[Act. %d] %s", vNum, fAct))

			desc := act.Descripcion
			if strings.TrimSpace(desc) == "" {
				desc = "Sin detalle especificado"
			}
			descList = append(descList, fmt.Sprintf("[Act. %d] %s", vNum, desc))
		}

		writer.Write([]string{
			fmt.Sprintf("%d", doc.ID),
			doc.Titulo,
			"Vigente",
			tipoDoc,
			area,
			gaveta,
			fmt.Sprintf("%d", doc.TotalPaginas),
			puestosStr,
			strings.Join(actasList, " // "),
			strings.Join(fechasList, " // "),
			strings.Join(descList, " // "),
			fmt.Sprintf("%d", len(doc.Actualizaciones)),
			doc.FechaCreacion.Format("2006-01-02 15:04"),
			doc.UltimaActualizacion.Format("2006-01-02 15:04"),
		})
	}

	return nil
}

