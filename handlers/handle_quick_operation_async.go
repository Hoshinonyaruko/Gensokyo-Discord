package handlers

import (
	"github.com/hoshinonyaruko/gensokyo-discord/callapi"
)

func init() {
	callapi.RegisterHandler(".handle_quick_operation_async", Handle_quick_operation)
}
