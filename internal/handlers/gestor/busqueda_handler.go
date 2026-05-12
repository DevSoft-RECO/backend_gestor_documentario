package gestor

import (
	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/gofiber/fiber/v2"
)

// BuscarDocumentoPorNumero busca en indices_paginas por numero_documento
func BuscarDocumentoPorNumero(c *fiber.Ctx) error {
	numero := c.Params("numero")
	if numero == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Debe proporcionar un número de documento"})
	}

	var resultados []models.IndicePagina

	// Buscamos coincidencias parciales (LIKE) para mayor flexibilidad
	err := db.DB.Preload("Documento.Asociado").
		Preload("Documento.Subcategoria.Categoria").
		Where("numero_documento LIKE ?", "%"+numero+"%").
		Order("fecha_operacion DESC").
		Find(&resultados).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al realizar la búsqueda", "detalle": err.Error()})
	}

	return c.JSON(resultados)
}
