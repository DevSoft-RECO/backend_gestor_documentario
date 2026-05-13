package models

type Agencia struct {
	ID        int    `gorm:"primaryKey" json:"id"`
	Nombre    string `gorm:"size:150" json:"nombre"`
	Codigo    *int   `json:"codigo"`
	CodigoT24 *string `gorm:"size:10" json:"codigot24"`
	Direccion *string `gorm:"size:255" json:"direccion"`
}

func (Agencia) TableName() string {
	return "agencias"
}

type Usuario struct {
	ID          int     `gorm:"primaryKey" json:"id"`
	Name        *string `gorm:"size:150" json:"name"`
	Username    *string `gorm:"size:100" json:"username"`
	Email       *string `gorm:"size:150" json:"email"`
	Telefono    *string `gorm:"size:20" json:"telefono"`
	IDAgencia   *int    `json:"id_agencia"`
	Avatar      *string `gorm:"size:255" json:"avatar"`
	JTI         *string `gorm:"size:100" json:"jti"`
	Roles       *string `json:"roles"`       // Store as JSON string
	Permissions *string `json:"permissions"` // Store as JSON string
	IDPuesto    *uint   `json:"id_puesto"`   // Relacionado con el Puesto
	Puesto      *Puesto `gorm:"foreignKey:IDPuesto" json:"puesto,omitempty"`
}

func (Usuario) TableName() string {
	return "usuarios"
}


