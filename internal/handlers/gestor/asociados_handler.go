package gestor

import (
	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/gofiber/fiber/v2"
)

// BuscarAsociado busca por nombre, DPI o código de cliente
func BuscarAsociado(c *fiber.Ctx) error {
	query := c.Query("q")
	if query == "" {
		return c.JSON([]models.Asociado{})
	}

	var asociados []models.Asociado
	searchQuery := "%" + query + "%"
	
	err := db.DB.Where("nombre_completo ILIKE ? OR dpi = ? OR codigo_cliente = ?", searchQuery, query, query).
		Limit(10).
		Find(&asociados).Error

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al buscar asociados"})
	}

	return c.JSON(asociados)
}

// CrearAsociado registra un nuevo asociado en el sistema
func CrearAsociado(c *fiber.Ctx) error {
	asociado := new(models.Asociado)
	if err := c.BodyParser(asociado); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	if err := db.DB.Create(asociado).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al registrar asociado"})
	}

	return c.Status(fiber.StatusCreated).JSON(asociado)
}

// ObtenerAsociado devuelve los datos de un asociado específico por ID
func ObtenerAsociado(c *fiber.Ctx) error {
	id := c.Params("id")
	var asociado models.Asociado
	
	if err := db.DB.First(&asociado, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Asociado no encontrado"})
	}

	return c.JSON(asociado)
}
