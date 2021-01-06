package api

import "github.com/go-chi/chi"

type Controller interface {
	Setup(chi.Router)
}

func InitializeControllers() ([]Controller, error) {
	return []Controller{
		NewPingController(),
	}, nil
}
