package gestor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
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

	// Contar páginas físicas
	masterPath := "." + documento.FilePath
	totalPaginas, err := api.PageCountFile(masterPath)
	if err != nil {
		totalPaginas = 0
	}
	
	return c.JSON(fiber.Map{
		"indices":        indices,
		"total_paginas": totalPaginas,
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
	if err := db.DB.First(&documento, documentoID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Documento maestro no encontrado"})
	}

	file, err := c.FormFile("documento")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Archivo PDF inválido"})
	}

	tempNewPath := filepath.Join(os.TempDir(), fmt.Sprintf("temp_insert_new_%d.pdf", time.Now().UnixNano()))
	if err := c.SaveFile(file, tempNewPath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al guardar archivo temporal"})
	}
	defer os.Remove(tempNewPath)

	masterPath := "." + documento.FilePath
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

	tx := db.DB.Begin()

	// 1. Físico
	input, _ := os.ReadFile(tempOutPath)
	if err := os.WriteFile(masterPath, input, 0644); err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al sobrescribir PDF físico"})
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
	if err := db.DB.First(&documento, documentoID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Documento no encontrado"})
	}

	file, err := c.FormFile("documento") // La nueva hoja
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Archivo PDF de reemplazo inválido"})
	}

	tempNewPath := filepath.Join(os.TempDir(), fmt.Sprintf("temp_rep_new_%d.pdf", time.Now().UnixNano()))
	if err := c.SaveFile(file, tempNewPath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error guardando temporal"})
	}
	defer os.Remove(tempNewPath)

	masterPath := "." + documento.FilePath
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

	tx := db.DB.Begin()

	// 1. Físico
	input, _ := os.ReadFile(tempOutPath)
	if err := os.WriteFile(masterPath, input, 0644); err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al sobrescribir PDF físico"})
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
	if err := db.DB.First(&documento, documentoID).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Documento no encontrado"})
	}

	masterPath := "." + documento.FilePath
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

	tx := db.DB.Begin()

	// 1. Físico
	input, _ := os.ReadFile(tempOutPath)
	if err := os.WriteFile(masterPath, input, 0644); err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al sobrescribir PDF físico"})
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
