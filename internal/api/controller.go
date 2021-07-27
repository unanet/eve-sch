package api

import (
	"github.com/go-chi/chi"

	"github.com/unanet/eve-sch/internal/service/callback"
)

type Controller interface {
	Setup(chi.Router)
}

func InitializeControllers(cm *callback.Manager) ([]Controller, error) {
	return []Controller{
		NewPingController(),
		NewCallbackController(cm),
	}, nil
}
