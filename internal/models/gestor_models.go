package models

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
	ID          uint   `gorm:"primaryKey" json:"id"`
	CategoriaID uint   `gorm:"not null" json:"categoria_id"`
	Nombre      string `gorm:"size:150;not null" json:"nombre"`
	Estado      bool   `gorm:"default:true" json:"estado"`
}

func (Subcategoria) TableName() string {
	return "subcategorias"
}
