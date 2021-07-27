package api

import (
	"net/http"

	"github.com/go-chi/chi"
	"github.com/go-chi/render"
	uuid "github.com/satori/go.uuid"
	"github.com/unanet/eve/pkg/eve"
	"github.com/unanet/go/pkg/errors"
	"github.com/unanet/go/pkg/json"

	"github.com/unanet/eve-sch/internal/service/callback"
)

type CallbackController struct {
	manager *callback.Manager
}

func NewCallbackController(manager *callback.Manager) *CallbackController {
	return &CallbackController{
		manager: manager,
	}
}

func (c CallbackController) Setup(r chi.Router) {
	r.Post("/callback", c.callback)
}

func (c CallbackController) callback(w http.ResponseWriter, r *http.Request) {
	var m eve.CallbackMessage
	if err := json.ParseBody(r, &m); err != nil {
		render.Respond(w, r, err)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		render.Respond(w, r, errors.NewRestError(400, "you must supply id as a query parameter"))
		return
	}

	jobID, err := uuid.FromString(id)
	if err != nil {
		render.Respond(w, r, errors.NewRestError(400, "id is invalid"))
		return
	}

	err = c.manager.Callback(r.Context(), jobID, m)
	if err != nil {
		render.Respond(w, r, err)
		return
	}
}
