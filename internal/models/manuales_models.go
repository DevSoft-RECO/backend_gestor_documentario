package models

import (
	"time"
)

type ManualCategoria struct {
	ID            uint                 `gorm:"primaryKey" json:"id"`
	Nombre        string               `gorm:"size:150;not null" json:"nombre"`
	Estado        bool                 `gorm:"default:true" json:"estado"` // true = Activo, false = Inactivo
	Subcategorias []ManualSubcategoria `gorm:"foreignKey:ManualCategoriaID" json:"subcategorias"`
}

func (ManualCategoria) TableName() string {
	return "manual_categorias"
}

type ManualSubcategoria struct {
	ID                uint              `gorm:"primaryKey" json:"id"`
	ManualCategoriaID uint              `gorm:"not null" json:"manual_categoria_id"`
	Nombre            string            `gorm:"size:150;not null" json:"nombre"`
	Estado            bool              `gorm:"default:true" json:"estado"`
	Categoria         ManualCategoria   `gorm:"foreignKey:ManualCategoriaID" json:"categoria"`
	Carpetas          []ManualCarpeta   `gorm:"foreignKey:ManualSubcategoriaID" json:"carpetas"`
}

func (ManualSubcategoria) TableName() string {
	return "manual_subcategorias"
}

type ManualCarpeta struct {
	ID                   uint              `gorm:"primaryKey" json:"id"`
	ManualSubcategoriaID uint              `gorm:"not null" json:"manual_subcategoria_id"`
	Nombre               string            `gorm:"size:150;not null" json:"nombre"`
	Estado               bool              `gorm:"default:true" json:"estado"`
	Subcategoria         ManualSubcategoria `gorm:"foreignKey:ManualSubcategoriaID" json:"subcategoria"`
	Documentos           []ManualDocumento `gorm:"foreignKey:ManualCarpetaID" json:"documentos"`
}

func (ManualCarpeta) TableName() string {
	return "manual_carpetas"
}

type ManualDocumento struct {
	ID                   uint               `gorm:"primaryKey" json:"id"`
	ManualCarpetaID      *uint              `json:"manual_carpeta_id"`
	Titulo               string             `gorm:"size:255;not null" json:"titulo"`
	FilePath             string             `gorm:"type:text;not null" json:"file_path"`
	TotalPaginas         int                `gorm:"default:0" json:"total_paginas"`
	FechaCreacion        time.Time          `gorm:"autoCreateTime" json:"fecha_creacion"`
	UltimaActualizacion  time.Time          `gorm:"autoUpdateTime" json:"ultima_actualizacion"`
	UsuarioID            uint               `gorm:"not null;default:1" json:"usuario_id"`
	NumeroActa           string             `gorm:"size:100" json:"numero_acta"`
	FechaAprobacion      *time.Time         `json:"fecha_aprobacion"`
	FechaVigencia        *time.Time         `json:"fecha_vigencia"`

	// Relaciones
	Carpeta      ManualCarpeta      `gorm:"foreignKey:ManualCarpetaID" json:"carpeta"`
	Usuario      Usuario            `gorm:"foreignKey:UsuarioID" json:"usuario"`

	// Relación muchos a muchos con Puestos para control de lectura granular
	PuestosAutorizados []Puesto `gorm:"many2many:manual_documento_puestos;" json:"puestos_autorizados"`

	// Relación uno a muchos con Hojas de Actualización (Cambios)
	Actualizaciones []ManualActualizacion `gorm:"foreignKey:ManualDocumentoID;constraint:OnDelete:CASCADE;" json:"actualizaciones"`
}

func (ManualDocumento) TableName() string {
	return "manual_documentos"
}

type ManualActualizacion struct {
	ID                uint       `gorm:"primaryKey" json:"id"`
	ManualDocumentoID uint       `gorm:"not null" json:"manual_documento_id"`
	NumeroActa        string     `gorm:"size:100;not null" json:"numero_acta"`
	FechaAprobacion   *time.Time `json:"fecha_aprobacion"`
	FechaVigencia     *time.Time `json:"fecha_vigencia"`
	Descripcion       string     `gorm:"type:text" json:"descripcion"`
	FilePath          string     `gorm:"type:text;not null" json:"file_path"`
	TotalPaginas      int        `gorm:"default:0" json:"total_paginas"`
	FechaCreacion     time.Time  `gorm:"autoCreateTime" json:"fecha_creacion"`
	UsuarioID         uint       `gorm:"not null;default:1" json:"usuario_id"`

	// Relación
	Usuario Usuario `gorm:"foreignKey:UsuarioID" json:"usuario"`
}

func (ManualActualizacion) TableName() string {
	return "manual_actualizaciones"
}
