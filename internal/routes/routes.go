package routes

import (
	"github.com/DevSoft-RECO/backend-creditos-go/internal/config"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/handlers"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/handlers/gestor"

	"github.com/gofiber/fiber/v2"
)

func SetupRoutes(app *fiber.App) {
	api := app.Group("/api")

	// Auth
	api.Get("/me", handlers.MeHandler)

	// Módulo Gestor Documental (Real)
	gestorGroup := api.Group("/gestor")
	gestorGroup.Get("/categorias", gestor.GetCategorias)
	gestorGroup.Post("/categorias", gestor.CreateCategoria)
	gestorGroup.Put("/categorias/:id", gestor.UpdateCategoria)

	gestorGroup.Post("/subcategorias", gestor.CreateSubcategoria)
	gestorGroup.Put("/subcategorias/:id", gestor.UpdateSubcategoria)

	// Asociados
	gestorGroup.Get("/asociados/search", gestor.BuscarAsociado)
	gestorGroup.Post("/asociados", gestor.CrearAsociado)
	gestorGroup.Get("/asociados/:id", gestor.ObtenerAsociado)

	// Servir archivos subidos
	app.Static("/uploads", "./uploads")

	// Health check
	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "ok",
			"app":    "Yaman Kutx Ecosistema API (Go)",
		})
	})

	// 🕷️ 8. Arquitectura Anti-JSON (Capa de redirección)
	app.Get("/login", func(c *fiber.Ctx) error {
		return c.Redirect(config.Envs.FrontendURL + "/login?session_expired=true")
	})
}
