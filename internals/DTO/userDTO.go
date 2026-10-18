package dto

import (
	"net/mail"
	"strings"
	"unicode/utf8"
)

type UserDTO struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginUserDTO struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginUserResponseDTO struct {
	Email    string `json:"email"`
	Username string `json:"username"`
}

func (u *UserDTO) Normalize() {
	u.Username = strings.TrimSpace(u.Username)
	u.Email = strings.ToLower(strings.TrimSpace(u.Email))
}

func (u UserDTO) ValidationErrors() map[string]string {
	errors := make(map[string]string)
	if utf8.RuneCountInString(u.Username) < 2 {
		errors["username"] = "username must be at least 2 characters"
	}
	address, err := mail.ParseAddress(u.Email)
	if u.Email == "" || err != nil || address.Address != u.Email {
		errors["email"] = "email must be valid"
	}
	if len([]byte(u.Password)) < 8 || len([]byte(u.Password)) > 72 {
		errors["password"] = "password must be between 8 and 72 bytes"
	}
	return errors
}
