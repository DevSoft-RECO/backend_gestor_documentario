package gestor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/config"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/gcs"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"gorm.io/gorm"
)

// ObtenerExpediente devuelve todos los documentos de un asociado,
// precargando la información de sus subcategorías y categorías.
func ObtenerExpediente(c *fiber.Ctx) error {
	asociadoID := c.Params("asociado_id")

	var documentos []models.Documento
	err := db.DB.Preload("Subcategoria").
		Preload("Usuario").
		Preload("Subcategoria.PuestosAutorizados").
		Preload("Subcategoria.Categoria", func(db *gorm.DB) *gorm.DB {
			return db.Select("ID, Nombre")
		}).
		Where("asociado_id = ?", asociadoID).
		Find(&documentos).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al obtener expediente"})
	}

	return c.JSON(documentos)
}

// SubirDocumento maneja la carga física del PDF y registra el puente en la BD.
func SubirDocumento(c *fiber.Ctx) error {
	asociadoIDStr := c.FormValue("asociado_id")
	subcategoriaIDStr := c.FormValue("subcategoria_id")

	if asociadoIDStr == "" || subcategoriaIDStr == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Faltan parámetros requeridos"})
	}

	asociadoID, err := strconv.ParseUint(asociadoIDStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de asociado inválido"})
	}

	subcategoriaID, err := strconv.ParseUint(subcategoriaIDStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de subcategoría inválido"})
	}

	file, err := c.FormFile("documento")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No se recibió ningún archivo o es inválido"})
	}

	// Obtener información de la subcategoría para nombrar el archivo y verificar permisos
	var subcategoria models.Subcategoria
	if err := db.DB.Preload("PuestosAutorizados").First(&subcategoria, subcategoriaID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Subcategoría no encontrada"})
	}

	// === VERIFICACIÓN DE PERMISOS POR PUESTO ===
	// 1. Obtener el usuario actual con su puesto y roles
	var usuarioID uint = 1 // Fallback
	isSuperAdmin := false
	if claims, ok := c.Locals("userClaims").(jwt.MapClaims); ok {
		// Verificar si es Super Admin o Administrador de forma robusta
		if rolesRaw, ok := claims["roles"]; ok {
			switch r := rolesRaw.(type) {
			case []interface{}:
				for _, role := range r {
					if s, ok := role.(string); ok {
						if s == "Super Admin" || s == "Administrador" || s == "Admin" {
							isSuperAdmin = true
							break
						}
					}
				}
			case string:
				if r == "Super Admin" || r == "Administrador" || r == "Admin" {
					isSuperAdmin = true
				}
			}
		}

		if sub, ok := claims["sub"]; ok {
			if idFloat, ok := sub.(float64); ok {
				usuarioID = uint(idFloat)
			} else if idStr, ok := sub.(string); ok {
				if parsed, err := strconv.ParseUint(idStr, 10, 32); err == nil {
					usuarioID = uint(parsed)
				}
			}
		}
	}

	// === VERIFICACIÓN DE PERMISOS POR PUESTO / ROL ===
	var userLocal models.Usuario
	if err := db.DB.Preload("Puesto").First(&userLocal, usuarioID).Error; err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Usuario no encontrado en la base local"})
	}

	// Verificar si es Super Admin desde la columna roles (JSON) de la tabla usuarios
	isSuperAdminDB := false
	if userLocal.Roles != nil {
		var roles []string
		// Intentamos decodificar el array JSON
		if err := json.Unmarshal([]byte(*userLocal.Roles), &roles); err == nil {
			for _, r := range roles {
				if r == "Super Admin" {
					isSuperAdminDB = true
					break
				}
			}
		} else {
			// Fallback si por alguna razón no es un array JSON puro
			if strings.Contains(*userLocal.Roles, "Super Admin") {
				isSuperAdminDB = true
			}
		}
	}

	// Si NO es Super Admin (ni por token ni por DB), aplicamos restricción por puesto
	if !isSuperAdmin && !isSuperAdminDB {
		// Si la subcategoría tiene restricciones de puestos
		if len(subcategoria.PuestosAutorizados) > 0 {
			authorized := false
			if userLocal.IDPuesto != nil {
				for _, p := range subcategoria.PuestosAutorizados {
					if p.PuestoID == *userLocal.IDPuesto && p.Editar {
						authorized = true
						break
					}
				}
			}
			if !authorized {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error":   "No tienes permiso para crear carpetas/documentos en esta subcategoría",
					"detalle": "Tu puesto no está autorizado para esta categoría.",
				})
			}
		}
	}
	// === FIN VERIFICACIÓN DE PERMISOS ===

	// Obtener el asociado para extraer su código de cliente
	var asociado models.Asociado
	if err := db.DB.First(&asociado, asociadoID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Asociado no encontrado"})
	}

	// Usar el Código de Cliente para el nombre del archivo. Fallback a DPI o ID.
	identificador := ""
	if asociado.CodigoCliente != nil && *asociado.CodigoCliente != "" {
		identificador = *asociado.CodigoCliente
	} else if asociado.DPI != "" {
		identificador = asociado.DPI
	} else {
		identificador = fmt.Sprintf("id_%d", asociadoID)
	}

	fileName := fmt.Sprintf("doc_subcat_%d_%s.pdf", subcategoriaID, identificador)
	gcsObjectName := fmt.Sprintf("%s/asociado_%d/%s", config.Envs.GCSPathPrefix, asociadoID, fileName)

	// Crear archivo temporal local para contar páginas
	tempFile, err := os.CreateTemp("", "upload_gcs_*.pdf")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al inicializar temporal"})
	}
	tempFilePath := tempFile.Name()
	defer os.Remove(tempFilePath)
	tempFile.Close()

	if err := c.SaveFile(file, tempFilePath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al guardar temporal local"})
	}

	// Contar páginas usando pdfcpu
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

	// Obtener el tamaño del archivo temporal
	var fileSize int64 = 0
	if fileInfo, errStat := os.Stat(tempFilePath); errStat == nil {
		fileSize = fileInfo.Size()
	}

	// Abrir el archivo temporal para subirlo a GCS
	fToUpload, err := os.Open(tempFilePath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al leer archivo temporal para subir"})
	}
	defer fToUpload.Close()

	// Subir archivo a GCS
	if err := gcs.SubirArchivo(c.UserContext(), gcsObjectName, fToUpload); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al subir archivo a la nube", "detalle": err.Error()})
	}

	// Buscar si ya existe un documento para esta combinación Asociado+Subcategoría
	var documento models.Documento
	result := db.DB.Where("asociado_id = ? AND subcategoria_id = ?", asociadoID, subcategoriaID).First(&documento)

	if result.Error == nil {
		// Ya existe: actualizar la ruta, total de páginas y el timestamp
		documento.FilePath = gcsObjectName
		documento.TotalPaginas = totalPaginas
		documento.Tamano = fileSize
		if err := db.DB.Save(&documento).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar registro de documento"})
		}

		// Como se sobreescribió todo el PDF físico, debemos eliminar todos los índices lógicos anteriores
		// para evitar desincronizaciones con las páginas del nuevo documento.
		db.DB.Where("documento_id = ?", documento.ID).Delete(&models.IndicePagina{})
	} else {
		// No existe: crearlo nuevo
		documento = models.Documento{
			AsociadoID:     uint(asociadoID),
			SubcategoriaID: uint(subcategoriaID),
			FilePath:       gcsObjectName,
			TotalPaginas:   totalPaginas,
			Tamano:         fileSize,
			UsuarioID:      usuarioID,
		}
		if err := db.DB.Create(&documento).Error; err != nil {
			fmt.Printf("[ERROR] Falló al crear documento en BD: %v\n", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al crear registro de documento", "detalle": err.Error()})
		}
	}

	// Crear índice inicial si se proporciona etiqueta
	etiqueta := c.FormValue("etiqueta")
	if etiqueta != "" {
		fechaVencimiento := c.FormValue("fecha_vencimiento")
		var fechaVencimientoPtr *time.Time
		if fechaVencimiento != "" {
			if parsedDate, err := time.Parse("2006-01-02", fechaVencimiento); err == nil {
				fechaVencimientoPtr = &parsedDate
			}
		}

		numeroDocumento := c.FormValue("numero_documento")
		var numDocPtr *string
		if numeroDocumento != "" {
			numDocPtr = &numeroDocumento
		}

		indice := models.IndicePagina{
			DocumentoID:      documento.ID,
			PaginaInicio:     1,
			TipoMovimiento:   "Documento Inicial",
			Etiqueta:         etiqueta,
			NumeroDocumento:  numDocPtr,
			UsuarioID:        usuarioID,
			FechaVencimiento: fechaVencimientoPtr,
		}
		db.DB.Create(&indice)
	}

	// Devolver el documento creado/actualizado
	// Precargamos la subcategoría para enviarla al frontend
	db.DB.Preload("Subcategoria").Preload("Usuario").First(&documento, documento.ID)

	return c.Status(fiber.StatusOK).JSON(documento)
}

// getUsuarioIDFromClaims extrae el ID del usuario de los claims de JWT.
func getUsuarioIDFromClaims(c *fiber.Ctx) uint {
	var usuarioID uint = 1 // Fallback
	if claims, ok := c.Locals("userClaims").(jwt.MapClaims); ok {
		if sub, ok := claims["sub"]; ok {
			if idFloat, ok := sub.(float64); ok {
				usuarioID = uint(idFloat)
			} else if idStr, ok := sub.(string); ok {
				if parsed, err := strconv.ParseUint(idStr, 10, 32); err == nil {
					usuarioID = uint(parsed)
				}
			}
		}
	}
	return usuarioID
}

// GenerarURLDocumento genera una URL firmada de 1 minuto para poder visualizar el documento de forma temporal.
func GenerarURLDocumento(c *fiber.Ctx) error {
	documentoIDStr := c.Params("documento_id")
	documentoID, err := strconv.ParseUint(documentoIDStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de documento inválido"})
	}

	var documento models.Documento
	if err := db.DB.Preload("Subcategoria.PuestosAutorizados").First(&documento, documentoID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Documento no encontrado"})
	}

	// === VERIFICACIÓN DE PERMISOS ===
	usuarioID := getUsuarioIDFromClaims(c)
	isSuperAdmin := false
	if claims, ok := c.Locals("userClaims").(jwt.MapClaims); ok {
		if rolesRaw, ok := claims["roles"]; ok {
			switch r := rolesRaw.(type) {
			case []interface{}:
				for _, role := range r {
					if s, ok := role.(string); ok {
						if s == "Super Admin" || s == "Administrador" || s == "Admin" || s == "Auditor" {
							isSuperAdmin = true
							break
						}
					}
				}
			case string:
				if r == "Super Admin" || r == "Administrador" || r == "Admin" || r == "Auditor" {
					isSuperAdmin = true
				}
			}
		}
	}

	var userLocal models.Usuario
	if err := db.DB.Preload("Puesto").First(&userLocal, usuarioID).Error; err == nil {
		isSuperAdminDB := false
		if userLocal.Roles != nil {
			var roles []string
			if err := json.Unmarshal([]byte(*userLocal.Roles), &roles); err == nil {
				for _, r := range roles {
					if r == "Super Admin" || r == "Auditor" || r == "Administrador" {
						isSuperAdminDB = true
						break
					}
				}
			} else if strings.Contains(*userLocal.Roles, "Super Admin") || strings.Contains(*userLocal.Roles, "Auditor") || strings.Contains(*userLocal.Roles, "Administrador") {
				isSuperAdminDB = true
			}
		}

		if !isSuperAdmin && !isSuperAdminDB {
			if len(documento.Subcategoria.PuestosAutorizados) > 0 {
				authorized := false
				if userLocal.IDPuesto != nil {
					for _, p := range documento.Subcategoria.PuestosAutorizados {
						if p.PuestoID == *userLocal.IDPuesto && (p.Ver || p.Editar) {
							authorized = true
							break
						}
					}
				}
				if !authorized {
					return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
						"error":   "No tienes permiso para visualizar este documento",
						"detalle": "Tu puesto no está autorizado.",
					})
				}
			}
		}
	}

	// Generar URL firmada de 1 minuto
	url, err := gcs.GenerarURLFirmada(documento.FilePath, 1*time.Minute)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Error al generar el enlace de visualización",
			"detalle": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"url": url,
	})
}

// EliminarDocumentoCompleto elimina un documento (carpeta de subcategoría), sus índices de páginas y su archivo físico de GCS
// DELETE /api/gestor/documentos/:documento_id/eliminar-completo
func EliminarDocumentoCompleto(c *fiber.Ctx) error {
	if !isUserSuperAdmin(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Acceso restringido a Super Administradores"})
	}

	docIDStr := c.Params("documento_id")
	docID, err := strconv.ParseUint(docIDStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de documento inválido"})
	}

	// 1. Buscar el documento
	var documento models.Documento
	if err := db.DB.First(&documento, docID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Documento no encontrado"})
	}

	// 2. Transacción de base de datos
	tx := db.DB.Begin()
	if tx.Error != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al iniciar la transacción"})
	}

	// Eliminar los índices de páginas relacionados con el documento
	if err := tx.Where("documento_id = ?", documento.ID).Delete(&models.IndicePagina{}).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al eliminar índices de páginas"})
	}

	// Eliminar el registro de documento
	if err := tx.Delete(&documento).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al eliminar registro de documento"})
	}

	if err := tx.Commit().Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al confirmar la eliminación"})
	}

	// 3. Eliminar el archivo físico de GCS en segundo plano
	if documento.FilePath != "" {
		go func(filePath string) {
			ctx := context.Background()
			_ = gcs.EliminarArchivo(ctx, filePath)
		}(documento.FilePath)
	}

	return c.JSON(fiber.Map{
		"message": "Documento eliminado correctamente del expediente, índices y archivo físico depurados",
	})
}

// Helper para verificar si el usuario es Admin o Super Admin de manera robusta
func isUserAdmin(c *fiber.Ctx) bool {
	if isUserSuperAdmin(c) {
		return true
	}

	claims, ok := c.Locals("userClaims").(jwt.MapClaims)
	if !ok {
		return false
	}

	// 1. Verificar Claims
	if rolesRaw, ok := claims["roles"]; ok {
		switch r := rolesRaw.(type) {
		case []interface{}:
			for _, role := range r {
				if s, ok := role.(string); ok && (s == "Administrador" || s == "Admin") {
					return true
				}
			}
		case string:
			if r == "Administrador" || r == "Admin" {
				return true
			}
		}
	}

	// 2. Verificar BD local
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
	if err := db.DB.Select("roles").First(&userLocal, usuarioID).Error; err == nil {
		if userLocal.Roles != nil {
			rolesStr := *userLocal.Roles
			if strings.Contains(rolesStr, "Administrador") || strings.Contains(rolesStr, "Admin") {
				return true
			}
		}
	}

	return false
}
