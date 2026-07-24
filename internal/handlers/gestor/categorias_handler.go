package gestor

import (
	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/gofiber/fiber/v2"
)

// === CATEGORIAS ===

func GetCategorias(c *fiber.Ctx) error {
	var categorias []models.Categoria
	if err := db.DB.Preload("Subcategorias").Preload("Subcategorias.PuestosAutorizados.Puesto").Find(&categorias).Error; err != nil {
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

	// Guardar cambios básicos
	if err := db.DB.Save(subcategoria).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al actualizar subcategoría"})
	}

	// Sincronizar SubcategoriaID en cada puesto
	for i := range subcategoria.PuestosAutorizados {
		subcategoria.PuestosAutorizados[i].SubcategoriaID = subcategoria.ID
	}

	// Transacción manual para evitar el problema de GORM al intentar poner subcategoria_id a NULL
	tx := db.DB.Begin()
	if err := tx.Where("subcategoria_id = ?", subcategoria.ID).Delete(&models.SubcategoriaPuesto{}).Error; err != nil {
		tx.Rollback()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al limpiar puestos autorizados"})
	}

	if len(subcategoria.PuestosAutorizados) > 0 {
		if err := tx.Create(&subcategoria.PuestosAutorizados).Error; err != nil {
			tx.Rollback()
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al guardar nuevos puestos autorizados"})
		}
	}
	tx.Commit()

	return c.JSON(subcategoria)
}
