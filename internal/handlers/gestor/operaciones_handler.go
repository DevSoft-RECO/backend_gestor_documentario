package gestor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func getUsuarioIDFromToken(c *fiber.Ctx) uint {
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

// ObtenerIndices devuelve los índices asociados a un documento y el total de páginas físicas.
func ObtenerIndices(c *fiber.Ctx) error {
	documentoID := c.Params("documento_id")

	var documento models.Documento
	if err := db.DB.First(&documento, documentoID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Documento no encontrado"})
	}

	var indices []models.IndicePagina
	err := db.DB.Where("documento_id = ?", documentoID).Order("pagina_inicio ASC").Find(&indices).Error
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al obtener índices"})
	}

	return c.JSON(fiber.Map{
		"indices":       indices,
		"total_paginas": documento.TotalPaginas,
	})
}

// InsertarPaginas inserta hojas nuevas después de una página específica. Si targetPage = total, anexa al final.
func InsertarPaginas(c *fiber.Ctx) error {
	documentoIDStr := c.Params("documento_id")
	documentoID, err := strconv.ParseUint(documentoIDStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de documento inválido"})
	}

	targetPageStr := c.FormValue("target_page")
	targetPage, err := strconv.Atoi(targetPageStr)
	if err != nil || targetPage < 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Página objetivo inválida"})
	}

	etiqueta := c.FormValue("etiqueta") // Puede ser vacía si no quiere dejar índice

	var documento models.Documento
	if err := db.DB.Preload("Subcategoria.PuestosAutorizados").First(&documento, documentoID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Documento Inicial no encontrado"})
	}

	// === VERIFICACIÓN DE PERMISOS ===
	usuarioID := getUsuarioIDFromToken(c)
	isSuperAdmin := false
	if claims, ok := c.Locals("userClaims").(jwt.MapClaims); ok {
		if rolesRaw, ok := claims["roles"]; ok {
			if s, ok := rolesRaw.(string); ok && s == "Super Admin" { isSuperAdmin = true }
			if ss, ok := rolesRaw.([]interface{}); ok {
				for _, r := range ss { if r == "Super Admin" { isSuperAdmin = true; break } }
			}
		}
	}

	var userLocal models.Usuario
	db.DB.First(&userLocal, usuarioID)
	isSuperAdminDB := false
	if userLocal.Roles != nil {
		var roles []string
		if err := json.Unmarshal([]byte(*userLocal.Roles), &roles); err == nil {
			for _, r := range roles { if r == "Super Admin" { isSuperAdminDB = true; break } }
		} else if strings.Contains(*userLocal.Roles, "Super Admin") { isSuperAdminDB = true }
	}

	if !isSuperAdmin && !isSuperAdminDB {
		if len(documento.Subcategoria.PuestosAutorizados) > 0 {
			authorized := false
			if userLocal.IDPuesto != nil {
				for _, p := range documento.Subcategoria.PuestosAutorizados {
					if p.ID == *userLocal.IDPuesto { authorized = true; break }
				}
			}
			if !authorized {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permiso para modificar este documento"})
			}
		}
	}
	// === FIN VERIFICACIÓN DE PERMISOS ===

	file, err := c.FormFile("documento")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Archivo PDF inválido"})
	}

	tempNewPath := filepath.Join(os.TempDir(), fmt.Sprintf("temp_insert_new_%d.pdf", time.Now().UnixNano()))
	if err := c.SaveFile(file, tempNewPath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al guardar archivo temporal"})
	}
	defer os.Remove(tempNewPath)

	// Descargar el documento maestro desde GCS a un archivo temporal local
	masterPath, err := gcs.DescargarArchivoTemporal(c.UserContext(), documento.FilePath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al descargar documento maestro de GCS", "detalle": err.Error()})
	}
	defer os.Remove(masterPath)

	tempOutPath := filepath.Join(os.TempDir(), fmt.Sprintf("temp_insert_out_%d.pdf", time.Now().UnixNano()))

	pageCountMaster, err := api.PageCountFile(masterPath)
	if err != nil {
		pageCountMaster = 0
	}

	if targetPage > pageCountMaster {
		targetPage = pageCountMaster // Fallback al final si la página es mayor al total
	}

	pageCountNew, err := api.PageCountFile(tempNewPath)
	if err != nil || pageCountNew == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El archivo a insertar no tiene páginas o es inválido"})
	}

	var partsToMerge []string

	// Si insertamos después de la página 0, va al principio. Si insertamos en medio, dividimos.
	if targetPage > 0 {
		part1 := filepath.Join(os.TempDir(), fmt.Sprintf("part1_%d.pdf", time.Now().UnixNano()))
		api.TrimFile(masterPath, part1, []string{fmt.Sprintf("1-%d", targetPage)}, nil)
		partsToMerge = append(partsToMerge, part1)
		defer os.Remove(part1)
	}

	partsToMerge = append(partsToMerge, tempNewPath)

	if targetPage < pageCountMaster {
		part3 := filepath.Join(os.TempDir(), fmt.Sprintf("part3_%d.pdf", time.Now().UnixNano()))
		api.TrimFile(masterPath, part3, []string{fmt.Sprintf("%d-%d", targetPage+1, pageCountMaster)}, nil)
		partsToMerge = append(partsToMerge, part3)
		defer os.Remove(part3)
	}

	if err := api.MergeCreateFile(partsToMerge, tempOutPath, false, nil); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error reconstruyendo PDF", "detalle": err.Error()})
	}
	defer os.Remove(tempOutPath)

	// Comprimir PDF antes de subirlo con Ghostscript
	tempOutPathComp := tempOutPath + ".compressed.pdf"
	if errComp := utils.CompressPDF(c.UserContext(), tempOutPath, tempOutPathComp); errComp == nil {
		tempOutPath = tempOutPathComp
		defer os.Remove(tempOutPathComp)
	} else {
		fmt.Printf("[WARN] No se pudo comprimir el PDF reconstruido con Ghostscript %s: %v\n", tempOutPath, errComp)
	}

	tx := db.DB.Begin()

	// 1. Físico (Subir a GCS)
	outFile, err := os.Open(tempOutPath)
	if err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al leer PDF reconstruido"})
	}
	defer outFile.Close()

	if err := gcs.SubirArchivo(c.UserContext(), documento.FilePath, outFile); err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al subir PDF reconstruido a la nube", "detalle": err.Error()})
	}

	// 1.5 Lógico: Actualizar total_paginas en BD
	if err := tx.Model(&documento).Update("total_paginas", pageCountMaster+pageCountNew).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar total de páginas en BD"})
	}

	// 2. Lógico: Actualizar índices posteriores sumando el número de hojas insertadas
	if err := tx.Model(&models.IndicePagina{}).
		Where("documento_id = ? AND pagina_inicio > ?", documento.ID, targetPage).
		Update("pagina_inicio", gorm.Expr("pagina_inicio + ?", pageCountNew)).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar índices posteriores"})
	}

	// Crear el nuevo índice si enviaron etiqueta
	var nuevoIndice *models.IndicePagina
	if etiqueta != "" {
		numeroDocumento := c.FormValue("numero_documento")
		var numDocPtr *string
		if numeroDocumento != "" {
			numDocPtr = &numeroDocumento
		}

		indice := models.IndicePagina{
			DocumentoID:     documento.ID,
			PaginaInicio:    targetPage + 1,
			TipoMovimiento:  "Inserción",
			Etiqueta:        etiqueta,
			NumeroDocumento: numDocPtr,
			UsuarioID:       getUsuarioIDFromToken(c),
		}
		fechaVencimiento := c.FormValue("fecha_vencimiento")
		if fechaVencimiento != "" {
			if parsedDate, err := time.Parse("2006-01-02", fechaVencimiento); err == nil {
				indice.FechaVencimiento = &parsedDate
			}
		}
		if err := tx.Create(&indice).Error; err != nil {
			tx.Rollback()
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al guardar el índice lógico"})
		}
		nuevoIndice = &indice
	}

	tx.Commit()
	return c.JSON(fiber.Map{"message": "Inserción exitosa", "indice": nuevoIndice})
}

// ReemplazarPaginaEspecifica reemplaza una hoja específica del PDF. No afecta los números de página de otros índices.
func ReemplazarPaginaEspecifica(c *fiber.Ctx) error {
	documentoIDStr := c.Params("documento_id")
	documentoID, err := strconv.ParseUint(documentoIDStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de documento inválido"})
	}

	targetPageStr := c.FormValue("target_page")
	targetPage, err := strconv.Atoi(targetPageStr)
	if err != nil || targetPage < 1 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Página objetivo inválida"})
	}

	var documento models.Documento
	if err := db.DB.Preload("Subcategoria.PuestosAutorizados").First(&documento, documentoID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Documento no encontrado"})
	}

	// === VERIFICACIÓN DE PERMISOS ===
	usuarioID_Rep := getUsuarioIDFromToken(c)
	isSuperAdmin_Rep := false
	if claims, ok := c.Locals("userClaims").(jwt.MapClaims); ok {
		if rolesRaw, ok := claims["roles"]; ok {
			if s, ok := rolesRaw.(string); ok && s == "Super Admin" { isSuperAdmin_Rep = true }
			if ss, ok := rolesRaw.([]interface{}); ok {
				for _, r := range ss { if r == "Super Admin" { isSuperAdmin_Rep = true; break } }
			}
		}
	}
	var userLocal_Rep models.Usuario
	db.DB.First(&userLocal_Rep, usuarioID_Rep)
	isSuperAdminDB_Rep := false
	if userLocal_Rep.Roles != nil {
		var roles []string
		if err := json.Unmarshal([]byte(*userLocal_Rep.Roles), &roles); err == nil {
			for _, r := range roles { if r == "Super Admin" { isSuperAdminDB_Rep = true; break } }
		} else if strings.Contains(*userLocal_Rep.Roles, "Super Admin") { isSuperAdminDB_Rep = true }
	}
	if !isSuperAdmin_Rep && !isSuperAdminDB_Rep {
		if len(documento.Subcategoria.PuestosAutorizados) > 0 {
			authorized := false
			if userLocal_Rep.IDPuesto != nil {
				for _, p := range documento.Subcategoria.PuestosAutorizados {
					if p.ID == *userLocal_Rep.IDPuesto { authorized = true; break }
				}
			}
			if !authorized {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permiso para modificar este documento"})
			}
		}
	}
	// === FIN VERIFICACIÓN DE PERMISOS ===

	file, err := c.FormFile("documento") // La nueva hoja
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Archivo PDF de reemplazo inválido"})
	}

	tempNewPath := filepath.Join(os.TempDir(), fmt.Sprintf("temp_rep_new_%d.pdf", time.Now().UnixNano()))
	if err := c.SaveFile(file, tempNewPath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error guardando temporal"})
	}
	defer os.Remove(tempNewPath)

	// Descargar el documento maestro desde GCS a un archivo temporal local
	masterPath, err := gcs.DescargarArchivoTemporal(c.UserContext(), documento.FilePath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al descargar documento maestro de GCS", "detalle": err.Error()})
	}
	defer os.Remove(masterPath)

	pageCount, err := api.PageCountFile(masterPath)
	if err != nil || targetPage > pageCount {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "La página indicada supera el total de páginas del documento"})
	}

	tempOutPath := filepath.Join(os.TempDir(), fmt.Sprintf("temp_rep_out_%d.pdf", time.Now().UnixNano()))

	var partsToMerge []string

	if targetPage > 1 {
		part1 := filepath.Join(os.TempDir(), fmt.Sprintf("part1_%d.pdf", time.Now().UnixNano()))
		api.TrimFile(masterPath, part1, []string{fmt.Sprintf("1-%d", targetPage-1)}, nil)
		partsToMerge = append(partsToMerge, part1)
		defer os.Remove(part1)
	}

	partsToMerge = append(partsToMerge, tempNewPath)

	if targetPage < pageCount {
		part3 := filepath.Join(os.TempDir(), fmt.Sprintf("part3_%d.pdf", time.Now().UnixNano()))
		api.TrimFile(masterPath, part3, []string{fmt.Sprintf("%d-%d", targetPage+1, pageCount)}, nil)
		partsToMerge = append(partsToMerge, part3)
		defer os.Remove(part3)
	}

	if err := api.MergeCreateFile(partsToMerge, tempOutPath, false, nil); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error reconstruyendo PDF", "detalle": err.Error()})
	}
	defer os.Remove(tempOutPath)

	// Comprimir PDF antes de subirlo con Ghostscript
	tempOutPathComp := tempOutPath + ".compressed.pdf"
	if errComp := utils.CompressPDF(c.UserContext(), tempOutPath, tempOutPathComp); errComp == nil {
		tempOutPath = tempOutPathComp
		defer os.Remove(tempOutPathComp)
	} else {
		fmt.Printf("[WARN] No se pudo comprimir el PDF reconstruido con Ghostscript %s: %v\n", tempOutPath, errComp)
	}

	tx := db.DB.Begin()

	// 1. Físico (Subir a GCS)
	outFile, err := os.Open(tempOutPath)
	if err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al leer PDF reconstruido"})
	}
	defer outFile.Close()

	if err := gcs.SubirArchivo(c.UserContext(), documento.FilePath, outFile); err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al subir PDF reconstruido a la nube", "detalle": err.Error()})
	}

	// 2. Lógico: Actualizar fecha y usuario si existe un índice apuntando a esta página exacta
	var indice models.IndicePagina
	if err := tx.Where("documento_id = ? AND pagina_inicio = ?", documento.ID, targetPage).First(&indice).Error; err == nil {
		indice.UsuarioID = getUsuarioIDFromToken(c)
		indice.TipoMovimiento = "Reemplazo"
		tx.Save(&indice)
	}

	tx.Commit()
	return c.JSON(fiber.Map{"message": "Página reemplazada exitosamente"})
}

// EliminarPaginaEspecifica elimina una página dada por número y actualiza índices en cascada.
func EliminarPaginaEspecifica(c *fiber.Ctx) error {
	documentoIDStr := c.Params("documento_id")
	documentoID, err := strconv.ParseUint(documentoIDStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de documento inválido"})
	}

	targetPageStr := c.FormValue("target_page")
	targetPage, err := strconv.Atoi(targetPageStr)
	if err != nil || targetPage < 1 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Página objetivo inválida"})
	}

	var documento models.Documento
	if err := db.DB.Preload("Subcategoria.PuestosAutorizados").First(&documento, documentoID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Documento no encontrado"})
	}

	// === VERIFICACIÓN DE PERMISOS ===
	usuarioID_Del := getUsuarioIDFromToken(c)
	isSuperAdmin_Del := false
	if claims, ok := c.Locals("userClaims").(jwt.MapClaims); ok {
		if rolesRaw, ok := claims["roles"]; ok {
			if s, ok := rolesRaw.(string); ok && s == "Super Admin" { isSuperAdmin_Del = true }
			if ss, ok := rolesRaw.([]interface{}); ok {
				for _, r := range ss { if r == "Super Admin" { isSuperAdmin_Del = true; break } }
			}
		}
	}
	var userLocal_Del models.Usuario
	db.DB.First(&userLocal_Del, usuarioID_Del)
	isSuperAdminDB_Del := false
	if userLocal_Del.Roles != nil {
		var roles []string
		if err := json.Unmarshal([]byte(*userLocal_Del.Roles), &roles); err == nil {
			for _, r := range roles { if r == "Super Admin" { isSuperAdminDB_Del = true; break } }
		} else if strings.Contains(*userLocal_Del.Roles, "Super Admin") { isSuperAdminDB_Del = true }
	}
	if !isSuperAdmin_Del && !isSuperAdminDB_Del {
		if len(documento.Subcategoria.PuestosAutorizados) > 0 {
			authorized := false
			if userLocal_Del.IDPuesto != nil {
				for _, p := range documento.Subcategoria.PuestosAutorizados {
					if p.ID == *userLocal_Del.IDPuesto { authorized = true; break }
				}
			}
			if !authorized {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No tienes permiso para modificar este documento"})
			}
		}
	}
	// === FIN VERIFICACIÓN DE PERMISOS ===

	// Descargar el documento maestro desde GCS a un archivo temporal local
	masterPath, err := gcs.DescargarArchivoTemporal(c.UserContext(), documento.FilePath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al descargar documento maestro de GCS", "detalle": err.Error()})
	}
	defer os.Remove(masterPath)

	pageCount, err := api.PageCountFile(masterPath)
	if err != nil || targetPage > pageCount {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "La página indicada es inválida o supera el total"})
	}

	if pageCount <= 1 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No se puede eliminar la única página del documento"})
	}

	tempOutPath := filepath.Join(os.TempDir(), fmt.Sprintf("temp_del_out_%d.pdf", time.Now().UnixNano()))

	var partsToMerge []string

	if targetPage > 1 {
		part1 := filepath.Join(os.TempDir(), fmt.Sprintf("part1_%d.pdf", time.Now().UnixNano()))
		api.TrimFile(masterPath, part1, []string{fmt.Sprintf("1-%d", targetPage-1)}, nil)
		partsToMerge = append(partsToMerge, part1)
		defer os.Remove(part1)
	}

	if targetPage < pageCount {
		part3 := filepath.Join(os.TempDir(), fmt.Sprintf("part3_%d.pdf", time.Now().UnixNano()))
		api.TrimFile(masterPath, part3, []string{fmt.Sprintf("%d-%d", targetPage+1, pageCount)}, nil)
		partsToMerge = append(partsToMerge, part3)
		defer os.Remove(part3)
	}

	if err := api.MergeCreateFile(partsToMerge, tempOutPath, false, nil); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error reconstruyendo PDF", "detalle": err.Error()})
	}
	defer os.Remove(tempOutPath)

	// Comprimir PDF antes de subirlo con Ghostscript
	tempOutPathComp := tempOutPath + ".compressed.pdf"
	if errComp := utils.CompressPDF(c.UserContext(), tempOutPath, tempOutPathComp); errComp == nil {
		tempOutPath = tempOutPathComp
		defer os.Remove(tempOutPathComp)
	} else {
		fmt.Printf("[WARN] No se pudo comprimir el PDF reconstruido con Ghostscript %s: %v\n", tempOutPath, errComp)
	}

	tx := db.DB.Begin()

	// 1. Físico (Subir a GCS)
	outFile, err := os.Open(tempOutPath)
	if err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al leer PDF reconstruido"})
	}
	defer outFile.Close()

	if err := gcs.SubirArchivo(c.UserContext(), documento.FilePath, outFile); err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al subir PDF reconstruido a la nube", "detalle": err.Error()})
	}

	// 1.5 Lógico: Actualizar total_paginas en BD
	if err := tx.Model(&documento).Update("total_paginas", pageCount-1).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar total de páginas en BD"})
	}

	// 2. Lógico: Eliminar el índice que apuntaba exactamente a esta página (si existe)
	tx.Where("documento_id = ? AND pagina_inicio = ?", documento.ID, targetPage).Delete(&models.IndicePagina{})

	// Operación en cascada: restar 1 a página de inicio de índices posteriores
	if err := tx.Model(&models.IndicePagina{}).
		Where("documento_id = ? AND pagina_inicio > ?", documento.ID, targetPage).
		Update("pagina_inicio", gorm.Expr("pagina_inicio - 1")).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar índices en cascada"})
	}

	tx.Commit()
	return c.JSON(fiber.Map{"message": "Página eliminada y cascada actualizada"})
}

// ActualizarIndice permite modificar la etiqueta, número de documento y fecha de vencimiento de un índice.
func ActualizarIndice(c *fiber.Ctx) error {
	// === VERIFICACIÓN DE ROL: SUPER ADMIN ===
	usuarioID := getUsuarioIDFromToken(c)
	isSuperAdmin := false
	if claims, ok := c.Locals("userClaims").(jwt.MapClaims); ok {
		if rolesRaw, ok := claims["roles"]; ok {
			if s, ok := rolesRaw.(string); ok && s == "Super Admin" { isSuperAdmin = true }
			if ss, ok := rolesRaw.([]interface{}); ok {
				for _, r := range ss { if r == "Super Admin" { isSuperAdmin = true; break } }
			}
		}
	}

	var userLocal models.Usuario
	db.DB.First(&userLocal, usuarioID)
	isSuperAdminDB := false
	if userLocal.Roles != nil {
		var roles []string
		if err := json.Unmarshal([]byte(*userLocal.Roles), &roles); err == nil {
			for _, r := range roles { if r == "Super Admin" { isSuperAdminDB = true; break } }
		} else if strings.Contains(*userLocal.Roles, "Super Admin") { isSuperAdminDB = true }
	}

	if !isSuperAdmin && !isSuperAdminDB {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No autorizado: Se requiere el rol de Super Admin"})
	}

	indiceIDStr := c.Params("id")
	indiceID, err := strconv.ParseUint(indiceIDStr, 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID de índice inválido"})
	}

	var input struct {
		Etiqueta         string  `json:"etiqueta"`
		NumeroDocumento  *string `json:"numero_documento"`
		FechaVencimiento *string `json:"fecha_vencimiento"`
	}

	if err := c.BodyParser(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Cuerpo de solicitud inválido"})
	}

	if strings.TrimSpace(input.Etiqueta) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "La etiqueta no puede estar vacía"})
	}

	var indice models.IndicePagina
	if err := db.DB.First(&indice, uint(indiceID)).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Índice no encontrado"})
	}

	indice.Etiqueta = strings.TrimSpace(input.Etiqueta)
	
	if input.NumeroDocumento != nil && strings.TrimSpace(*input.NumeroDocumento) != "" {
		trimmedNum := strings.TrimSpace(*input.NumeroDocumento)
		indice.NumeroDocumento = &trimmedNum
	} else {
		indice.NumeroDocumento = nil
	}

	if input.FechaVencimiento != nil && strings.TrimSpace(*input.FechaVencimiento) != "" {
		dateStr := strings.TrimSpace(*input.FechaVencimiento)
		if parsedDate, err := time.Parse("2006-01-02", dateStr); err == nil {
			indice.FechaVencimiento = &parsedDate
		} else {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Fecha de vencimiento con formato inválido (debe ser AAAA-MM-DD)"})
		}
	} else {
		indice.FechaVencimiento = nil
	}

	if err := db.DB.Save(&indice).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar el índice"})
	}

	return c.JSON(fiber.Map{"message": "Índice actualizado exitosamente", "indice": indice})
}

