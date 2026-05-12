package models

import (
	"time"
)

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
	FechaCreacion       time.Time    `gorm:"autoCreateTime" json:"fecha_creacion"`
	UltimaActualizacion time.Time    `gorm:"autoUpdateTime" json:"ultima_actualizacion"`

	// Relaciones
	Asociado     Asociado     `gorm:"foreignKey:AsociadoID" json:"asociado"`
	Subcategoria Subcategoria `gorm:"foreignKey:SubcategoriaID" json:"subcategoria"`
}

func (Documento) TableName() string {
	return "documentos"
}
