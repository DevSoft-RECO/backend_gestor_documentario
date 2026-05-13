package gestor

import (
	"time"

	"github.com/DevSoft-RECO/backend-creditos-go/internal/db"
	"github.com/gofiber/fiber/v2"
)

// --- Estructuras de Respuesta ---

type MonthlyCount struct {
	Mes   string `json:"mes"`
	Total int64  `json:"total"`
}

type CategoryCount struct {
	Nombre string `json:"nombre"`
	Total  int64  `json:"total"`
}

type ActividadItem struct {
	Etiqueta       string    `json:"etiqueta"`
	TipoMovimiento string    `json:"tipo_movimiento"`
	FechaOperacion time.Time `json:"fecha_operacion"`
	UsuarioNombre  string    `json:"usuario_nombre"`
	AsociadoNombre string    `json:"asociado_nombre"`
}

type AlertaVencimiento struct {
	Etiqueta         string    `json:"etiqueta"`
	NumeroDocumento  *string   `json:"numero_documento"`
	FechaVencimiento time.Time `json:"fecha_vencimiento"`
	AsociadoNombre   string    `json:"asociado_nombre"`
	Subcategoria     string    `json:"subcategoria"`
}

type DashboardStats struct {
	TotalAsociados         int64               `json:"total_asociados"`
	TotalDocumentos        int64               `json:"total_documentos"`
	TotalIndices           int64               `json:"total_indices"`
	DocsPorVencer          int64               `json:"docs_por_vencer"`
	CategoriasActivas      int64               `json:"categorias_activas"`
	OperacionesMes         int64               `json:"operaciones_mes"`
	DocumentosPorMes       []MonthlyCount      `json:"documentos_por_mes"`
	DocumentosPorCategoria []CategoryCount     `json:"documentos_por_categoria"`
	ActividadReciente      []ActividadItem     `json:"actividad_reciente"`
	AlertasVencimiento     []AlertaVencimiento `json:"alertas_vencimiento"`
}

// GetDashboardStats devuelve las métricas consolidadas del sistema.
func GetDashboardStats(c *fiber.Ctx) error {
	var stats DashboardStats

	// 1. Total Asociados
	db.DB.Table("asociados").Count(&stats.TotalAsociados)

	// 2. Total Documentos
	db.DB.Table("documentos").Count(&stats.TotalDocumentos)

	// 3. Total Índices
	db.DB.Table("indices_paginas").Count(&stats.TotalIndices)

	// 4. Documentos por Vencer (próximos 30 días)
	ahora := time.Now()
	en30Dias := ahora.AddDate(0, 0, 30)
	db.DB.Table("indices_paginas").
		Where("fecha_vencimiento IS NOT NULL AND fecha_vencimiento BETWEEN ? AND ?", ahora, en30Dias).
		Count(&stats.DocsPorVencer)

	// 5. Categorías Activas
	db.DB.Table("categorias").Where("estado = ?", true).Count(&stats.CategoriasActivas)

	// 6. Operaciones del Mes Actual
	inicioMes := time.Date(ahora.Year(), ahora.Month(), 1, 0, 0, 0, 0, ahora.Location())
	db.DB.Table("indices_paginas").
		Where("fecha_operacion >= ?", inicioMes).
		Count(&stats.OperacionesMes)

	// 7. Documentos creados por mes (últimos 6 meses)
	var docsPorMes []MonthlyCount
	seisAtras := ahora.AddDate(0, -6, 0)
	db.DB.Table("documentos").
		Select("TO_CHAR(fecha_creacion, 'YYYY-MM') AS mes, COUNT(*) AS total").
		Where("fecha_creacion >= ?", seisAtras).
		Group("mes").
		Order("mes ASC").
		Scan(&docsPorMes)

	// Fallback MySQL: si TO_CHAR no funciona (es PostgreSQL), intentar DATE_FORMAT
	if len(docsPorMes) == 0 {
		db.DB.Table("documentos").
			Select("DATE_FORMAT(fecha_creacion, '%Y-%m') AS mes, COUNT(*) AS total").
			Where("fecha_creacion >= ?", seisAtras).
			Group("mes").
			Order("mes ASC").
			Scan(&docsPorMes)
	}
	stats.DocumentosPorMes = docsPorMes

	// 8. Documentos por Categoría
	var docsPorCategoria []CategoryCount
	db.DB.Table("documentos").
		Select("categorias.nombre AS nombre, COUNT(documentos.id) AS total").
		Joins("JOIN subcategorias ON documentos.subcategoria_id = subcategorias.id").
		Joins("JOIN categorias ON subcategorias.categoria_id = categorias.id").
		Group("categorias.nombre").
		Order("total DESC").
		Scan(&docsPorCategoria)
	stats.DocumentosPorCategoria = docsPorCategoria

	// 9. Actividad Reciente (últimas 10 operaciones)
	var actividad []ActividadItem
	db.DB.Table("indices_paginas").
		Select("indices_paginas.etiqueta, indices_paginas.tipo_movimiento, indices_paginas.fecha_operacion, usuarios.name AS usuario_nombre, asociados.nombre_completo AS asociado_nombre").
		Joins("JOIN documentos ON indices_paginas.documento_id = documentos.id").
		Joins("JOIN usuarios ON indices_paginas.usuario_id = usuarios.id").
		Joins("JOIN asociados ON documentos.asociado_id = asociados.id").
		Order("indices_paginas.fecha_operacion DESC").
		Limit(10).
		Scan(&actividad)
	stats.ActividadReciente = actividad

	// 10. Alertas de Vencimiento (próximos 30 días)
	var alertas []AlertaVencimiento
	db.DB.Table("indices_paginas").
		Select("indices_paginas.etiqueta, indices_paginas.numero_documento, indices_paginas.fecha_vencimiento, asociados.nombre_completo AS asociado_nombre, subcategorias.nombre AS subcategoria").
		Joins("JOIN documentos ON indices_paginas.documento_id = documentos.id").
		Joins("JOIN asociados ON documentos.asociado_id = asociados.id").
		Joins("JOIN subcategorias ON documentos.subcategoria_id = subcategorias.id").
		Where("indices_paginas.fecha_vencimiento IS NOT NULL AND indices_paginas.fecha_vencimiento BETWEEN ? AND ?", ahora, en30Dias).
		Order("indices_paginas.fecha_vencimiento ASC").
		Scan(&alertas)
	stats.AlertasVencimiento = alertas

	return c.JSON(stats)
}
