package models

import (
	"time"
)

type Puesto struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	Nombre string `gorm:"size:150;not null;uniqueIndex" json:"nombre"`
}

func (Puesto) TableName() string {
	return "puestos"
}

type Categoria struct {
	ID            uint           `gorm:"primaryKey" json:"id"`
	Nombre        string         `gorm:"size:150;not null" json:"nombre"`
	Estado        bool           `gorm:"default:true" json:"estado"` // true = Activo, false = Inactivo
	Subcategorias []Subcategoria `gorm:"foreignKey:CategoriaID" json:"subcategorias"`
}

func (Categoria) TableName() string {
	return "categorias"
}

type Subcategoria struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	CategoriaID uint      `gorm:"not null" json:"categoria_id"`
	Nombre      string    `gorm:"size:150;not null" json:"nombre"`
	Estado      bool      `gorm:"default:true" json:"estado"`
	Categoria   Categoria `gorm:"foreignKey:CategoriaID" json:"categoria"`

	// Relación muchos a muchos con Puestos
	PuestosAutorizados []Puesto `gorm:"many2many:subcategoria_puestos;" json:"puestos_autorizados"`
}

func (Subcategoria) TableName() string {
	return "subcategorias"
}

type Asociado struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	CodigoCliente  *string   `gorm:"size:50" json:"codigo_cliente"`
	DPI            string    `gorm:"size:20;uniqueIndex" json:"dpi"`
	NombreCompleto string    `gorm:"size:255;not null" json:"nombre_completo"`
	Direccion      string    `gorm:"type:text" json:"direccion"`
	FechaRegistro  time.Time `gorm:"autoCreateTime" json:"fecha_registro"`
}

func (Asociado) TableName() string {
	return "asociados"
}

type Documento struct {
	ID                  uint         `gorm:"primaryKey" json:"id"`
	AsociadoID          uint         `gorm:"not null;uniqueIndex:idx_asociado_subcategoria" json:"asociado_id"`
	SubcategoriaID      uint         `gorm:"not null;uniqueIndex:idx_asociado_subcategoria" json:"subcategoria_id"`
	FilePath            string       `gorm:"type:text;not null" json:"file_path"`
	TotalPaginas        int          `gorm:"default:0" json:"total_paginas"`
	FechaCreacion       time.Time    `gorm:"autoCreateTime" json:"fecha_creacion"`
	UltimaActualizacion time.Time    `gorm:"autoUpdateTime" json:"ultima_actualizacion"`
	UsuarioID           uint         `gorm:"not null;default:1" json:"usuario_id"` // Quien creó el documento inicialmente

	// Relaciones
	Asociado     Asociado       `gorm:"foreignKey:AsociadoID" json:"asociado"`
	Subcategoria Subcategoria   `gorm:"foreignKey:SubcategoriaID" json:"subcategoria"`
	Usuario      Usuario        `gorm:"foreignKey:UsuarioID" json:"usuario"`
	Indices      []IndicePagina `gorm:"foreignKey:DocumentoID" json:"indices"`
}

func (Documento) TableName() string {
	return "documentos"
}

type IndicePagina struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	DocumentoID      uint       `gorm:"not null" json:"documento_id"`
	PaginaInicio     int        `gorm:"not null" json:"pagina_inicio"`
	TipoMovimiento   string     `gorm:"size:50;not null" json:"tipo_movimiento"`
	Etiqueta         string     `gorm:"size:255;not null" json:"etiqueta"`
	NumeroDocumento  *string    `gorm:"size:100" json:"numero_documento"`
	FechaVencimiento *time.Time `json:"fecha_vencimiento"`
	FechaOperacion   time.Time  `gorm:"autoCreateTime" json:"fecha_operacion"`
	UsuarioID        uint       `gorm:"not null;default:1" json:"usuario_id"`

	// Relaciones
	Documento Documento `gorm:"foreignKey:DocumentoID" json:"documento"`
}

func (IndicePagina) TableName() string {
	return "indices_paginas"
}
