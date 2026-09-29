package formatos

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/config"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/gcs"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func getGCSFormatosPath(areaID uint, filename string) string {
	prefix := strings.Trim(config.Envs.GCSPathPrefix, "/")
	cleanName := sanitizeFileName(filename)
	timestamp := time.Now().Unix()
	if prefix != "" {
		return fmt.Sprintf("%s/App_Formatos/area_%d/%d_%s", prefix, areaID, timestamp, cleanName)
	}
	return fmt.Sprintf("App_Formatos/area_%d/%d_%s", areaID, timestamp, cleanName)
}

func sanitizeFileName(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, " ", "_"))
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			sb.WriteRune(r)
		}
	}
	res := sb.String()
	if res == "" {
		res = "archivo"
	}
	return res
}

// === SEGURIDAD Y HELPERS DE SESIÓN ===

func isUserAdminFormatos(c *fiber.Ctx) bool {
	claims, ok := c.Locals("userClaims").(jwt.MapClaims)
	if !ok {
		return false
	}

	// 1. Roles en Token
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

	// 2. Permisos en Token
	if permsRaw, ok := claims["permissions"]; ok {
		switch p := permsRaw.(type) {
		case []interface{}:
			for _, perm := range p {
				if s, ok := perm.(string); ok && (s == "admin_formatos" || s == "admin_biblioteca") {
					return true
				}
			}
		case string:
			if p == "admin_formatos" || p == "admin_biblioteca" {
				return true
			}
		}
	}

	// 3. Respaldo en BD local
	usuarioID := getUsuarioID(c)
	if usuarioID == 0 {
		return false
	}

	var userLocal models.Usuario
	if err := db.DB.Select("roles, permissions").First(&userLocal, usuarioID).Error; err != nil {
		return false
	}

	if userLocal.Roles != nil && strings.Contains(*userLocal.Roles, "Super Admin") {
		return true
	}
	if userLocal.Permissions != nil && (strings.Contains(*userLocal.Permissions, "admin_formatos") || strings.Contains(*userLocal.Permissions, "admin_biblioteca")) {
		return true
	}

	return false
}

func getUsuarioID(c *fiber.Ctx) uint {
	claims, ok := c.Locals("userClaims").(jwt.MapClaims)
	if !ok {
		return 0
	}
	if sub, ok := claims["sub"]; ok {
		if idFloat, ok := sub.(float64); ok {
			return uint(idFloat)
		} else if idStr, ok := sub.(string); ok {
			if parsed, err := strconv.ParseUint(idStr, 10, 32); err == nil {
				return uint(parsed)
			}
		}
	}
	return 0
}

func getUsuarioPuestoID(c *fiber.Ctx) uint {
	usuarioID := getUsuarioID(c)
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

// === ÁREAS / DEPARTAMENTOS DE FORMATOS ===

func GetAreas(c *fiber.Ctx) error {
	var areas []models.FormatoArea
	if err := db.DB.Order("nombre ASC").Find(&areas).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al consultar áreas"})
	}
	return c.JSON(areas)
}

func CreateArea(c *fiber.Ctx) error {
	if !isUserAdminFormatos(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración de formatos"})
	}
	area := new(models.FormatoArea)
	if err := c.BodyParser(area); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}
	if strings.TrimSpace(area.Nombre) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El nombre del área es requerido"})
	}
	if err := db.DB.Create(area).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al crear área"})
	}
	return c.Status(fiber.StatusCreated).JSON(area)
}

func UpdateArea(c *fiber.Ctx) error {
	if !isUserAdminFormatos(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}
	id := c.Params("id")
	var area models.FormatoArea
	if err := db.DB.First(&area, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Área no encontrada"})
	}
	if err := c.BodyParser(&area); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}
	if strings.TrimSpace(area.Nombre) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El nombre es requerido"})
	}
	if err := db.DB.Save(&area).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar área"})
	}
	return c.JSON(area)
}

func DeleteArea(c *fiber.Ctx) error {
	if !isUserAdminFormatos(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}
	id := c.Params("id")
	var count int64
	db.DB.Model(&models.FormatoDocumento{}).Where("formato_area_id = ?", id).Count(&count)
	if count > 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No puedes eliminar un área con formatos registrados"})
	}
	if err := db.DB.Delete(&models.FormatoArea{}, id).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al eliminar área"})
	}
	return c.JSON(fiber.Map{"message": "Área eliminada exitosamente"})
}

// === CATÁLOGO PARA COLABORADORES (CON CONTROL DE DESCARGA POR PUESTO) ===

type FormatoCatalogoDTO struct {
	ID                  uint       `json:"id"`
	FormatoAreaID       uint       `json:"formato_area_id"`
	AreaNombre          string     `json:"area_nombre"`
	Codigo              string     `json:"codigo"`
	Titulo              string     `json:"titulo"`
	Descripcion         string     `json:"descripcion"`
	TipoArchivo         string     `json:"tipo_archivo"`
	TotalPaginas        int        `json:"total_paginas"`
	Version             string     `json:"version"`
	FechaAprobacion     *time.Time `json:"fecha_aprobacion"`
	FechaVigencia       *time.Time `json:"fecha_vigencia"`
	TotalDescargas      int        `json:"total_descargas"`
	PuedeVer            bool       `json:"puede_ver"`
	PuedeDescargar      bool       `json:"puede_descargar"` // <- REGLA POR PUESTO
	UltimaActualizacion time.Time  `json:"ultima_actualizacion"`
}

func GetCatalogoFormatos(c *fiber.Ctx) error {
	isAdmin := isUserAdminFormatos(c)
	puestoID := getUsuarioPuestoID(c)
	search := strings.TrimSpace(c.Query("search", ""))
	areaID := strings.TrimSpace(c.Query("area_id", ""))

	var formatos []models.FormatoDocumento
	query := db.DB.Model(&models.FormatoDocumento{}).
		Preload("Area").
		Preload("PuestosConfig").
		Where("formato_documentos.estado = ?", true)

	if areaID != "" {
		query = query.Where("formato_area_id = ?", areaID)
	}

	if search != "" {
		s := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(codigo) LIKE ? OR LOWER(titulo) LIKE ? OR LOWER(descripcion) LIKE ?", s, s, s)
	}

	if err := query.Order("codigo ASC, titulo ASC").Find(&formatos).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al obtener catálogo de formatos"})
	}

	resultado := make([]FormatoCatalogoDTO, 0)

	for _, doc := range formatos {
		puedeVer := false
		puedeDescargar := false

		if isAdmin {
			puedeVer = true
			puedeDescargar = true
		} else {
			// Si no tiene puestos configurados, se considera de acceso general
			if len(doc.PuestosConfig) == 0 {
				puedeVer = true
				puedeDescargar = true // Acceso general
			} else {
				// Buscar si el puesto del usuario tiene regla
				for _, pc := range doc.PuestosConfig {
					if pc.PuestoID == puestoID {
						puedeVer = pc.PuedeVer
						puedeDescargar = pc.PuedeDescargar
						break
					}
				}
			}
		}

		// Solo incluir si tiene autorización de visualización
		if puedeVer {
			areaNombre := "General"
			if doc.Area.Nombre != "" {
				areaNombre = doc.Area.Nombre
			}

			resultado = append(resultado, FormatoCatalogoDTO{
				ID:                  doc.ID,
				FormatoAreaID:       doc.FormatoAreaID,
				AreaNombre:          areaNombre,
				Codigo:              doc.Codigo,
				Titulo:              doc.Titulo,
				Descripcion:         doc.Descripcion,
				TipoArchivo:         doc.TipoArchivo,
				TotalPaginas:        doc.TotalPaginas,
				Version:             doc.Version,
				FechaAprobacion:     doc.FechaAprobacion,
				FechaVigencia:       doc.FechaVigencia,
				TotalDescargas:      doc.TotalDescargas,
				PuedeVer:            puedeVer,
				PuedeDescargar:      puedeDescargar,
				UltimaActualizacion: doc.UltimaActualizacion,
			})
		}
	}

	return c.JSON(resultado)
}

// === ADMINISTRACIÓN DE FORMATOS ===

func GetAdminFormatos(c *fiber.Ctx) error {
	if !isUserAdminFormatos(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	search := strings.TrimSpace(c.Query("search", ""))
	areaID := strings.TrimSpace(c.Query("area_id", ""))
	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.Query("limit", "15"))
	if limit < 1 {
		limit = 15
	}
	offset := (page - 1) * limit

	query := db.DB.Model(&models.FormatoDocumento{})

	if areaID != "" {
		query = query.Where("formato_area_id = ?", areaID)
	}

	if search != "" {
		s := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(codigo) LIKE ? OR LOWER(titulo) LIKE ?", s, s)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al contar formatos"})
	}

	var documentos []models.FormatoDocumento
	err := query.
		Preload("Area").
		Preload("Usuario").
		Preload("PuestosConfig.Puesto").
		Order("id DESC").
		Limit(limit).
		Offset(offset).
		Find(&documentos).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al consultar formatos"})
	}

	return c.JSON(fiber.Map{
		"documentos": documentos,
		"total":      total,
		"page":       page,
		"limit":      limit,
	})
}

// SubirFormato crea un nuevo formato con archivo físico y permisos por puesto
func SubirFormato(c *fiber.Ctx) error {
	if !isUserAdminFormatos(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	file, err := c.FormFile("documento")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El archivo es obligatorio"})
	}

	formatoAreaIDStr := c.FormValue("formato_area_id")
	codigo := strings.TrimSpace(c.FormValue("codigo"))
	titulo := strings.TrimSpace(c.FormValue("titulo"))
	descripcion := strings.TrimSpace(c.FormValue("descripcion"))
	version := strings.TrimSpace(c.FormValue("version", "1.0"))
	fechaAprobacionStr := c.FormValue("fecha_aprobacion")
	fechaVigenciaStr := c.FormValue("fecha_vigencia")
	puestosConfigStr := c.FormValue("puestos_config", "[]")

	if formatoAreaIDStr == "" || codigo == "" || titulo == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Área, Código y Título son campos obligatorios"})
	}

	areaID, _ := strconv.ParseUint(formatoAreaIDStr, 10, 32)

	// Validar extensión
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(file.Filename), "."))
	tipoArchivo := ext
	if ext != "pdf" && ext != "docx" && ext != "xlsx" && ext != "doc" && ext != "xls" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Solo se permiten formatos PDF, Word (.docx) o Excel (.xlsx)"})
	}

	// Guardar temporalmente para procesar
	tempPath := filepath.Join(os.TempDir(), fmt.Sprintf("upload_formato_%d_%s", time.Now().UnixNano(), file.Filename))
	if err := c.SaveFile(file, tempPath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al procesar archivo temporal"})
	}
	defer os.Remove(tempPath)

	// Si es PDF, calcular total de páginas
	totalPaginas := 0
	if ext == "pdf" {
		if pCount, err := api.PageCountFile(tempPath); err == nil {
			totalPaginas = pCount
		}
	}

	// Subir a GCS
	f, err := os.Open(tempPath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al leer archivo para GCS"})
	}
	defer f.Close()

	gcsPath := getGCSFormatosPath(uint(areaID), file.Filename)

	if err := gcs.SubirArchivo(c.Context(), gcsPath, f); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("Error al subir a GCS: %v", err)})
	}

	// Fechas
	var fechaAprobacion *time.Time
	if fechaAprobacionStr != "" {
		if p, err := time.Parse("2006-01-02", fechaAprobacionStr); err == nil {
			fechaAprobacion = &p
		}
	}

	var fechaVigencia *time.Time
	if fechaVigenciaStr != "" {
		if p, err := time.Parse("2006-01-02", fechaVigenciaStr); err == nil {
			fechaVigencia = &p
		}
	}

	usuarioID := getUsuarioID(c)
	if usuarioID == 0 {
		usuarioID = 1
	}

	formato := models.FormatoDocumento{
		FormatoAreaID:   uint(areaID),
		Codigo:          codigo,
		Titulo:          titulo,
		Descripcion:     descripcion,
		FilePath:        gcsPath,
		TipoArchivo:     tipoArchivo,
		TotalPaginas:    totalPaginas,
		Version:         version,
		FechaAprobacion: fechaAprobacion,
		FechaVigencia:   fechaVigencia,
		Estado:          true,
		UsuarioID:       usuarioID,
	}

	if err := db.DB.Create(&formato).Error; err != nil {
		gcs.EliminarArchivo(c.Context(), gcsPath)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al registrar formato en la base de datos"})
	}

	// Parsear y guardar permisos por puesto
	type PuestoConfigDTO struct {
		PuestoID       uint `json:"puesto_id"`
		PuedeVer       bool `json:"puede_ver"`
		PuedeDescargar bool `json:"puede_descargar"`
	}
	var configs []PuestoConfigDTO
	if err := json.Unmarshal([]byte(puestosConfigStr), &configs); err == nil {
		for _, cfg := range configs {
			if cfg.PuestoID > 0 {
				puestoRule := models.FormatoDocumentoPuesto{
					FormatoDocumentoID: formato.ID,
					PuestoID:           cfg.PuestoID,
					PuedeVer:           cfg.PuedeVer,
					PuedeDescargar:     cfg.PuedeDescargar,
				}
				db.DB.Create(&puestoRule)
			}
		}
	}

	return c.Status(fiber.StatusCreated).JSON(formato)
}

// UpdateFormato actualiza los metadatos o reemplaza el archivo de un formato
func UpdateFormato(c *fiber.Ctx) error {
	if !isUserAdminFormatos(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	id := c.Params("id")
	var formato models.FormatoDocumento
	if err := db.DB.First(&formato, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Formato no encontrado"})
	}

	formatoAreaIDStr := c.FormValue("formato_area_id")
	codigo := strings.TrimSpace(c.FormValue("codigo"))
	titulo := strings.TrimSpace(c.FormValue("titulo"))
	descripcion := strings.TrimSpace(c.FormValue("descripcion"))
	version := strings.TrimSpace(c.FormValue("version"))
	fechaAprobacionStr := c.FormValue("fecha_aprobacion")
	fechaVigenciaStr := c.FormValue("fecha_vigencia")
	puestosConfigStr := c.FormValue("puestos_config")

	if formatoAreaIDStr != "" {
		if areaID, err := strconv.ParseUint(formatoAreaIDStr, 10, 32); err == nil {
			formato.FormatoAreaID = uint(areaID)
		}
	}
	if codigo != "" {
		formato.Codigo = codigo
	}
	if titulo != "" {
		formato.Titulo = titulo
	}
	formato.Descripcion = descripcion
	if version != "" {
		formato.Version = version
	}

	if fechaAprobacionStr != "" {
		if p, err := time.Parse("2006-01-02", fechaAprobacionStr); err == nil {
			formato.FechaAprobacion = &p
		}
	} else {
		formato.FechaAprobacion = nil
	}

	if fechaVigenciaStr != "" {
		if p, err := time.Parse("2006-01-02", fechaVigenciaStr); err == nil {
			formato.FechaVigencia = &p
		}
	} else {
		formato.FechaVigencia = nil
	}

	// Reemplazo opcional de archivo físico
	file, err := c.FormFile("documento")
	if err == nil && file != nil {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(file.Filename), "."))
		if ext != "pdf" && ext != "docx" && ext != "xlsx" && ext != "doc" && ext != "xls" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Solo se permiten formatos PDF, Word o Excel"})
		}

		tempPath := filepath.Join(os.TempDir(), fmt.Sprintf("update_formato_%d_%s", time.Now().UnixNano(), file.Filename))
		if err := c.SaveFile(file, tempPath); err == nil {
			defer os.Remove(tempPath)

			totalPaginas := 0
			if ext == "pdf" {
				if pCount, err := api.PageCountFile(tempPath); err == nil {
					totalPaginas = pCount
				}
			}

			if f, err := os.Open(tempPath); err == nil {
				defer f.Close()
				gcsPath := getGCSFormatosPath(formato.FormatoAreaID, file.Filename)

				if err := gcs.SubirArchivo(c.Context(), gcsPath, f); err == nil {
					// Eliminar anterior de GCS
					gcs.EliminarArchivo(c.Context(), formato.FilePath)
					formato.FilePath = gcsPath
					formato.TipoArchivo = ext
					formato.TotalPaginas = totalPaginas
				}
			}
		}
	}

	if err := db.DB.Save(&formato).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar formato"})
	}

	// Actualizar reglas de puestos si se enviaron
	if puestosConfigStr != "" {
		type PuestoConfigDTO struct {
			PuestoID       uint `json:"puesto_id"`
			PuedeVer       bool `json:"puede_ver"`
			PuedeDescargar bool `json:"puede_descargar"`
		}
		var configs []PuestoConfigDTO
		if err := json.Unmarshal([]byte(puestosConfigStr), &configs); err == nil {
			// Borrar anteriores
			db.DB.Where("formato_documento_id = ?", formato.ID).Delete(&models.FormatoDocumentoPuesto{})
			// Insertar nuevas
			for _, cfg := range configs {
				if cfg.PuestoID > 0 {
					puestoRule := models.FormatoDocumentoPuesto{
						FormatoDocumentoID: formato.ID,
						PuestoID:           cfg.PuestoID,
						PuedeVer:           cfg.PuedeVer,
						PuedeDescargar:     cfg.PuedeDescargar,
					}
					db.DB.Create(&puestoRule)
				}
			}
		}
	}

	return c.JSON(formato)
}

func DeleteFormato(c *fiber.Ctx) error {
	if !isUserAdminFormatos(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permisos de administración"})
	}

	id := c.Params("id")
	var formato models.FormatoDocumento
	if err := db.DB.First(&formato, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Formato no encontrado"})
	}

	// Eliminar de GCS
	gcs.EliminarArchivo(c.Context(), formato.FilePath)

	// Eliminar de BD (cascade elimina puestos_config)
	if err := db.DB.Delete(&formato).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al eliminar formato"})
	}

	return c.JSON(fiber.Map{"message": "Formato eliminado exitosamente"})
}

// === ACCESO SEGURO (VER / DESCARGAR) ===

// VerFormato genera URL temporal para previsualización (PDFs)
func VerFormato(c *fiber.Ctx) error {
	id := c.Params("id")
	var formato models.FormatoDocumento
	if err := db.DB.Preload("PuestosConfig").First(&formato, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Formato no encontrado"})
	}

	isAdmin := isUserAdminFormatos(c)
	puestoID := getUsuarioPuestoID(c)

	if !isAdmin {
		// Validar si tiene permiso de ver
		if len(formato.PuestosConfig) > 0 {
			authorized := false
			for _, pc := range formato.PuestosConfig {
				if pc.PuestoID == puestoID && pc.PuedeVer {
					authorized = true
					break
				}
			}
			if !authorized {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Tu puesto no tiene autorización para ver este formato"})
			}
		}
	}

	url, err := gcs.GenerarURLFirmada(formato.FilePath, 2*time.Minute)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al generar enlace seguro"})
	}

	return c.JSON(fiber.Map{
		"url":          url,
		"tipo_archivo": formato.TipoArchivo,
	})
}

// DescargarFormato valida el permiso de DESCARGA por puesto antes de permitir bajar el archivo
func DescargarFormato(c *fiber.Ctx) error {
	id := c.Params("id")
	var formato models.FormatoDocumento
	if err := db.DB.Preload("PuestosConfig").First(&formato, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Formato no encontrado"})
	}

	isAdmin := isUserAdminFormatos(c)
	puestoID := getUsuarioPuestoID(c)

	// === CONTROL ESTRICTO DE DESCARGA POR PUESTO ===
	if !isAdmin {
		if len(formato.PuestosConfig) > 0 {
			puedeDescargar := false
			for _, pc := range formato.PuestosConfig {
				if pc.PuestoID == puestoID && pc.PuedeDescargar {
					puedeDescargar = true
					break
				}
			}
			if !puedeDescargar {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "Tu puesto de trabajo no cuenta con autorización para descargar este formato institucional.",
				})
			}
		}
	}

	// Incrementar contador de descargas
	db.DB.Model(&formato).UpdateColumn("total_descargas", formato.TotalDescargas+1)

	// Generar URL firmada
	url, err := gcs.GenerarURLFirmada(formato.FilePath, 5*time.Minute)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al generar enlace de descarga"})
	}

	return c.JSON(fiber.Map{
		"url":      url,
		"codigo":   formato.Codigo,
		"filename": fmt.Sprintf("%s_%s.%s", formato.Codigo, sanitizeFileName(formato.Titulo), formato.TipoArchivo),
	})
}

// GetPuestosFormatos lista los puestos para configurar permisos
func GetPuestosFormatos(c *fiber.Ctx) error {
	var puestos []models.Puesto
	if err := db.DB.Order("nombre ASC").Find(&puestos).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al obtener puestos"})
	}
	return c.JSON(puestos)
}
