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
	Documentos        []ManualDocumento `gorm:"foreignKey:ManualSubcategoriaID" json:"documentos"`
}

func (ManualSubcategoria) TableName() string {
	return "manual_subcategorias"
}

type ManualDocumento struct {
	ID                   uint               `gorm:"primaryKey" json:"id"`
	ManualSubcategoriaID uint               `gorm:"not null" json:"manual_subcategoria_id"`
	Titulo               string             `gorm:"size:255;not null" json:"titulo"`
	FilePath             string             `gorm:"type:text;not null" json:"file_path"`
	TotalPaginas         int                `gorm:"default:0" json:"total_paginas"`
	FechaCreacion        time.Time          `gorm:"autoCreateTime" json:"fecha_creacion"`
	UltimaActualizacion  time.Time          `gorm:"autoUpdateTime" json:"ultima_actualizacion"`
	UsuarioID            uint               `gorm:"not null;default:1" json:"usuario_id"`

	// Relaciones
	Subcategoria ManualSubcategoria `gorm:"foreignKey:ManualSubcategoriaID" json:"subcategoria"`
	Usuario      Usuario            `gorm:"foreignKey:UsuarioID" json:"usuario"`

	// Relación muchos a muchos con Puestos para control de lectura granular
	PuestosAutorizados []Puesto `gorm:"many2many:manual_documento_puestos;" json:"puestos_autorizados"`
}

func (ManualDocumento) TableName() string {
	return "manual_documentos"
}
