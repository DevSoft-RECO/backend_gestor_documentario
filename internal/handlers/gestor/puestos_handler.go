package gestor

import (
	"fmt"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/auth"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/gofiber/fiber/v2"
)

// GetPuestos retorna la lista local de puestos
func GetPuestos(c *fiber.Ctx) error {
	var puestos []models.Puesto
	if err := db.DB.Order("nombre asc").Find(&puestos).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al obtener puestos locales"})
	}
	return c.JSON(puestos)
}

// SyncPuestos sincroniza activamente los puestos desde la App Madre
func SyncPuestos(c *fiber.Ctx) error {
	rawAuth := c.Get("Authorization")
	if rawAuth == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "No se proporcionó token"})
	}

	// 1. Consultar a la App Madre usando el cliente centralizado
	motherPuestosRaw, err := auth.FetchPuestosFromMother(rawAuth)
	if err != nil {
		fmt.Printf("[ERROR SyncPuestos] %v\n", err)
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error":   "Error en la App Madre al obtener puestos",
			"details": err.Error(),
		})
	}

	// 2. Upsert local (Sincronización Activa)
	count := 0
	for _, item := range motherPuestosRaw {
		if m, ok := item.(map[string]interface{}); ok {
			nombre, _ := m["nombre"].(string)
			idFloat, _ := m["id"].(float64)
			id := uint(idFloat)

			if nombre == "" || id == 0 {
				continue
			}

			// Forzamos el ID de la madre localmente para mantener la integridad
			p := models.Puesto{ID: id, Nombre: nombre}
			if err := db.DB.Save(&p).Error; err == nil {
				count++
			}
		}
	}

	return c.JSON(fiber.Map{
		"message":            "Sincronización completada",
		"puestos_procesados": count,
	})
}
