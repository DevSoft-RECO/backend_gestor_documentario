package routes

import (
	"github.com/DevSoft-RECO/backend-creditos-go/internal/config"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/handlers"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/handlers/gestor"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/handlers/manuales"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

func SetupRoutes(app *fiber.App) {
	api := app.Group("/api")

	// Auth
	api.Get("/me", handlers.MeHandler)

	// Módulo Gestor Documental (Real) - Protegido
	gestorGroup := api.Group("/gestor", middleware.AuthRequired)
	gestorGroup.Get("/categorias", gestor.GetCategorias)
	gestorGroup.Post("/categorias", gestor.CreateCategoria)
	gestorGroup.Put("/categorias/:id", gestor.UpdateCategoria)

	gestorGroup.Post("/subcategorias", gestor.CreateSubcategoria)
	gestorGroup.Put("/subcategorias/:id", gestor.UpdateSubcategoria)

	// Puestos (Sincronización y listado)
	gestorGroup.Get("/puestos", gestor.GetPuestos)
	gestorGroup.Post("/puestos/sync", gestor.SyncPuestos)

	// Asociados
	gestorGroup.Get("/asociados/search", gestor.BuscarAsociado)
	gestorGroup.Post("/asociados", gestor.CrearAsociado)
	gestorGroup.Get("/asociados/:id", gestor.ObtenerAsociado)
	gestorGroup.Get("/asociados/:id/actividad", gestor.ObtenerActividadAsociado)
	gestorGroup.Put("/asociados/:id", gestor.UpdateAsociado)
	gestorGroup.Get("/admin/asociados", gestor.GetAdminAsociados)
	gestorGroup.Delete("/admin/asociados/:id", gestor.DeleteAdminAsociado)

	// Documentos (Expediente)
	gestorGroup.Get("/asociados/:asociado_id/expediente", gestor.ObtenerExpediente)
	gestorGroup.Post("/documentos/upload", gestor.SubirDocumento)
	gestorGroup.Get("/documentos/:documento_id/url", gestor.GenerarURLDocumento)

	// Operaciones Quirúrgicas (Índices y Manipulación por Página)
	gestorGroup.Get("/documentos/:documento_id/indices", gestor.ObtenerIndices)
	gestorGroup.Post("/documentos/:documento_id/insertar", gestor.InsertarPaginas)
	gestorGroup.Post("/documentos/:documento_id/reemplazar", gestor.ReemplazarPaginaEspecifica)
	gestorGroup.Delete("/documentos/:documento_id/eliminar", gestor.EliminarPaginaEspecifica)
	gestorGroup.Get("/busqueda/documento/:numero", gestor.BuscarDocumentoPorNumero)
	gestorGroup.Get("/dashboard/stats", gestor.GetDashboardStats)

	// Módulo Independiente de Manuales (Biblioteca de Documentación) - Protegido
	manualesGroup := api.Group("/manuales", middleware.AuthRequired)
	// Lectores generales (por puesto)
	manualesGroup.Get("/biblioteca", manuales.GetBibliotecaManuales)
	manualesGroup.Get("/documentos/:id/url", manuales.GenerarURLManual)
	// Admin (Super Admin o permiso admin_biblioteca)
	manualesGroup.Get("/admin/categorias", manuales.GetAdminCategorias)
	manualesGroup.Post("/categorias", manuales.CreateCategoria)
	manualesGroup.Put("/categorias/:id", manuales.UpdateCategoria)
	manualesGroup.Post("/subcategorias", manuales.CreateSubcategoria)
	manualesGroup.Put("/subcategorias/:id", manuales.UpdateSubcategoria)
	manualesGroup.Post("/carpetas", manuales.CreateCarpeta)
	manualesGroup.Put("/carpetas/:id", manuales.UpdateCarpeta)
	manualesGroup.Post("/documentos/upload", manuales.SubirManual)
	manualesGroup.Put("/documentos/:id", manuales.UpdateManual)
	manualesGroup.Delete("/documentos/:id", manuales.DeleteManual)

	// Hojas de Actualización Versionadas
	manualesGroup.Post("/documentos/:id/actualizaciones", manuales.SubirActualizacion)
	manualesGroup.Delete("/actualizaciones/:id", manuales.DeleteActualizacion)
	manualesGroup.Get("/actualizaciones/:id/url", manuales.GenerarURLActualizacion)

	// Servir archivos subidos (Deshabilitado: migrado a almacenamiento GCS con URLs firmadas temporales)
	// app.Static("/uploads", "./uploads")

	// Health check
	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "ok",
			"app":    "Yaman Kutx Ecosistema API (Go)",
		})
	})

	// === BACKUP SYSTEM ===
	// Rutas internas de respaldo llamadas por la APP_MADRE (Firmadas con HMAC)
	api.Post("/internal/backup", handlers.GenerateBackupHandler)
	api.Delete("/internal/backup", handlers.DeleteBackupHandler)
	api.Get("/internal/download-backup", handlers.DownloadBackupHandler)

	// 🕷️ 8. Arquitectura Anti-JSON (Capa de redirección)
	app.Get("/login", func(c *fiber.Ctx) error {
		return c.Redirect(config.Envs.FrontendURL + "/login?session_expired=true")
	})
}
