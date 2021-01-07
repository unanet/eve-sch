package api

import (
	"net/http"

	"github.com/go-chi/chi"
	"github.com/go-chi/render"
	uuid "github.com/satori/go.uuid"
	"gitlab.unanet.io/devops/go/pkg/errors"
	"gitlab.unanet.io/devops/go/pkg/json"

	"gitlab.unanet.io/devops/eve-sch/internal/service/callback"
	"gitlab.unanet.io/devops/eve-sch/pkg/sdk"
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
	var m sdk.CallbackMessage
	if err := json.ParseBody(r, &m); err != nil {
		render.Respond(w, r, err)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		render.Respond(w, r, errors.NewRestError(400, "you must pay id as a query parameter"))
		return
	}

	jobID, err := uuid.FromString(id)
	if err != nil {
		render.Respond(w, r, errors.NewRestError(400, "id is invalid"))
	}

	err = c.manager.Callback(r.Context(), jobID, m)
	if err != nil {
		render.Respond(w, r, err)
	}

	render.Status(r, http.StatusAccepted)
}
