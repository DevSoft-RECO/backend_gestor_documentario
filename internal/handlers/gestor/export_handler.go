package gestor

import (
	"encoding/csv"
	"fmt"
	"time"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/DevSoft-RECO/backend-creditos-go/internal/models"
	"github.com/gofiber/fiber/v2"
)

func ExportarReporteUnificado(c *fiber.Ctx) error {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	query := db.DB.Model(&models.Documento{}).
		Preload("Asociado").
		Preload("Subcategoria.Categoria").
		Preload("Usuario").
		Preload("Indices")

	if startDate != "" && endDate != "" {
		query = query.Where("fecha_creacion BETWEEN ? AND ?", startDate+" 00:00:00", endDate+" 23:59:59")
	}

	var documentos []models.Documento
	if err := query.Find(&documentos).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error al consultar documentos"})
	}

	c.Set("Content-Type", "text/csv")
	c.Set("Content-Disposition", "attachment; filename=reporte_unificado.csv")

	writer := csv.NewWriter(c.Response().BodyWriter())
	defer writer.Flush()

	// Header
	writer.Write([]string{
		"ID Expediente", "DPI Asociado", "Nombre Asociado", "Portafolio", "Folder",
		"Páginas Físicas", "Tamaño (MB)", "Fecha de Carga", "Usuario Creación",
		"Etiqueta", "Fecha Vencimiento", "Días Restantes", "Estado Vencimiento",
	})

	now := time.Now()

	for _, doc := range documentos {
		catNombre := ""
		if doc.Subcategoria.Categoria.Nombre != "" {
			catNombre = doc.Subcategoria.Categoria.Nombre
		}

		usuarioNombre := "Desconocido"
		if doc.Usuario.Name != nil {
			usuarioNombre = *doc.Usuario.Name
		}

		tamanoMB := float64(doc.Tamano) / 1024.0 / 1024.0
		tamanoStr := fmt.Sprintf("%.2f MB", tamanoMB)

		// Base row data
		baseRow := []string{
			fmt.Sprintf("%d", doc.ID),
			doc.Asociado.DPI,
			doc.Asociado.NombreCompleto,
			catNombre,
			doc.Subcategoria.Nombre,
			fmt.Sprintf("%d", doc.TotalPaginas),
			tamanoStr,
			doc.FechaCreacion.Format("2006-01-02 15:04:05"),
			usuarioNombre,
		}

		if len(doc.Indices) == 0 {
			// Write document without specific index info
			row := append(baseRow, "N/A", "N/A", "N/A", "N/A")
			writer.Write(row)
		} else {
			for _, idx := range doc.Indices {
				fechaVencimientoStr := "No aplica"
				diasRestantesStr := "N/A"
				estadoVencimiento := "N/A"

				if idx.FechaVencimiento != nil {
					fechaVencimientoStr = idx.FechaVencimiento.Format("2006-01-02")
					dias := int(idx.FechaVencimiento.Sub(now).Hours() / 24)
					diasRestantesStr = fmt.Sprintf("%d", dias)

					estadoVencimiento = "Vigente"
					if dias < 0 {
						estadoVencimiento = "Vencido"
					} else if dias <= 30 {
						estadoVencimiento = "Por Vencer"
					}
				}

				row := append(append([]string(nil), baseRow...), idx.Etiqueta, fechaVencimientoStr, diasRestantesStr, estadoVencimiento)
				writer.Write(row)
			}
		}
	}

	return nil
}
