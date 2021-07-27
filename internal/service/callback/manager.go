package callback

import (
	"context"
	"encoding/json"

	uuid "github.com/satori/go.uuid"
	"github.com/unanet/eve/pkg/eve"
	"github.com/unanet/eve/pkg/queue"
	"github.com/unanet/go/pkg/errors"

	"github.com/unanet/eve-sch/internal/service"
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
