package models

import (
	"time"
)

// FormatoArea representa el área o departamento que emite los formatos institucionales (ej: Créditos, Captaciones, RRHH)
type FormatoArea struct {
	ID         uint               `gorm:"primaryKey" json:"id"`
	Nombre     string             `gorm:"size:150;not null;uniqueIndex" json:"nombre"`
	Estado     bool               `gorm:"default:true" json:"estado"` // true = Activo, false = Inactivo
	Documentos []FormatoDocumento `gorm:"foreignKey:FormatoAreaID" json:"documentos,omitempty"`
}

func (FormatoArea) TableName() string {
	return "formato_areas"
}

// FormatoDocumento representa un formato o plantilla institucional oficial
type FormatoDocumento struct {
	ID                  uint       `gorm:"primaryKey" json:"id"`
	FormatoAreaID       uint       `gorm:"not null" json:"formato_area_id"`
	Codigo              string     `gorm:"size:50;not null;index" json:"codigo"`
	Titulo              string     `gorm:"size:255;not null" json:"titulo"`
	Descripcion         string     `gorm:"type:text" json:"descripcion"`
	FilePath            string     `gorm:"type:text;not null" json:"file_path"`
	TipoArchivo         string     `gorm:"size:20;not null;default:'pdf'" json:"tipo_archivo"` // pdf, docx, xlsx
	TotalPaginas        int        `gorm:"default:0" json:"total_paginas"`
	Version             string     `gorm:"size:20;default:'1.0'" json:"version"`
	FechaAprobacion     *time.Time `json:"fecha_aprobacion"`
	FechaVigencia       *time.Time `json:"fecha_vigencia"`
	Estado              bool       `gorm:"default:true" json:"estado"` // true = Vigente, false = Obsoleto
	TotalDescargas      int        `gorm:"default:0" json:"total_descargas"`
	UsuarioID           uint       `gorm:"not null;default:1" json:"usuario_id"`
	FechaCreacion       time.Time  `gorm:"autoCreateTime" json:"fecha_creacion"`
	UltimaActualizacion time.Time  `gorm:"autoUpdateTime" json:"ultima_actualizacion"`

	// Relaciones
	Area          FormatoArea              `gorm:"foreignKey:FormatoAreaID" json:"area"`
	Usuario       Usuario                  `gorm:"foreignKey:UsuarioID" json:"usuario"`
	PuestosConfig []FormatoDocumentoPuesto `gorm:"foreignKey:FormatoDocumentoID;constraint:OnDelete:CASCADE;" json:"puestos_config"`
}

func (FormatoDocumento) TableName() string {
	return "formato_documentos"
}

// FormatoDocumentoPuesto define los permisos granulares por puesto sobre cada formato: visualización y descarga
type FormatoDocumentoPuesto struct {
	FormatoDocumentoID uint   `gorm:"primaryKey;autoIncrement:false" json:"formato_documento_id"`
	PuestoID           uint   `gorm:"primaryKey;autoIncrement:false" json:"puesto_id"`
	PuedeVer           bool   `gorm:"default:true" json:"puede_ver"`
	PuedeDescargar     bool   `gorm:"default:false" json:"puede_descargar"` // Control de descarga por puesto
	Puesto             Puesto `gorm:"foreignKey:PuestoID" json:"puesto,omitempty"`
}

func (FormatoDocumentoPuesto) TableName() string {
	return "formato_documento_puestos"
}
