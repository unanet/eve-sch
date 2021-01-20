package callback

import (
	"context"
	"encoding/json"

	uuid "github.com/satori/go.uuid"
	"gitlab.unanet.io/devops/eve/pkg/eve"
	"gitlab.unanet.io/devops/eve/pkg/queue"
	"gitlab.unanet.io/devops/go/pkg/errors"

	"gitlab.unanet.io/devops/eve-sch/internal/service"
)

func NewManager(w *queue.Worker, apiQUrl string) *Manager {
	return &Manager{
		worker:  w,
		apiQUrl: apiQUrl,
	}
}

type Manager struct {
	worker  *queue.Worker
	apiQUrl string
}

func (m *Manager) Callback(ctx context.Context, id uuid.UUID, message eve.CallbackMessage) error {
	messageJson, err := json.Marshal(message)
	if err != nil {
		return errors.Wrap(err)
	}

	err = m.worker.Message(ctx, m.apiQUrl, &queue.M{
		ID:       id,
		GroupID:  service.CommandCallbackMessage,
		Body:     messageJson,
		Command:  service.CommandCallbackMessage,
		DedupeID: uuid.NewV4().String(),
	})

	if err != nil {
		return errors.Wrap(err)
	}

	return nil
}
