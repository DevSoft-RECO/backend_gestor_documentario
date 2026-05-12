package gestor

import (
	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/gofiber/fiber/v2"
)

// === CATEGORIAS ===

func GetCategorias(c *fiber.Ctx) error {
	var categorias []models.Categoria
	if err := db.DB.Preload("Subcategorias").Find(&categorias).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al obtener categorías"})
	}
	return c.JSON(categorias)
}

func CreateCategoria(c *fiber.Ctx) error {
	categoria := new(models.Categoria)
	if err := c.BodyParser(categoria); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	if err := db.DB.Create(categoria).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al crear categoría"})
	}

	return c.Status(fiber.StatusCreated).JSON(categoria)
}

func UpdateCategoria(c *fiber.Ctx) error {
	id := c.Params("id")
	categoria := new(models.Categoria)
	
	if err := db.DB.First(categoria, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Categoría no encontrada"})
	}

	if err := c.BodyParser(categoria); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	if err := db.DB.Save(categoria).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar categoría"})
	}

	return c.JSON(categoria)
}

// === SUBCATEGORIAS ===

func CreateSubcategoria(c *fiber.Ctx) error {
	subcategoria := new(models.Subcategoria)
	if err := c.BodyParser(subcategoria); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	if err := db.DB.Create(subcategoria).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al crear subcategoría"})
	}

	return c.Status(fiber.StatusCreated).JSON(subcategoria)
}

func UpdateSubcategoria(c *fiber.Ctx) error {
	id := c.Params("id")
	subcategoria := new(models.Subcategoria)

	if err := db.DB.First(subcategoria, id).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Subcategoría no encontrada"})
	}

	if err := c.BodyParser(subcategoria); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	if err := db.DB.Save(subcategoria).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar subcategoría"})
	}

	return c.JSON(subcategoria)
}
