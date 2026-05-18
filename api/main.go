package main

import (
	"fmt"
	"log"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/auth"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/config"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/gcs"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/routes"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func main() {
	// 1. Load Config
	config.LoadConfig()

	// 2. Connect DB
	db.ConnectDB()

	// 2.5. Initialize GCS Client
	if err := gcs.InitGCS(); err != nil {
		log.Printf("[WARN] No se pudo inicializar GCS: %v. El almacenamiento en la nube fallará.", err)
	}

	// 3. Load Auth Keys
	if err := auth.LoadPublicKey(); err != nil {
		log.Printf("[WARN] No se pudo cargar la llave pública: %v. La validación de JWT fallará.", err)
	}

	// 4. Init Fiber
	app := fiber.New(fiber.Config{
		AppName:   "APP9 Gestor Documentario — Backend Go",
		BodyLimit: 100 * 1024 * 1024, // Limite de 100MB para subida de archivos (Documentos)
	})

	// 5. Middleware
	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: config.Envs.AllowedOrigins,
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
	}))

	// 6. Routes
	routes.SetupRoutes(app)

	// 7. Start Server
	port := config.Envs.Port
	fmt.Printf("Servidor arrancando en puerto %s...\n", port)
	if err := app.Listen(fmt.Sprintf(":%s", port)); err != nil {
		log.Fatalf("Error al arrancar el servidor: %v", err)
	}
}
