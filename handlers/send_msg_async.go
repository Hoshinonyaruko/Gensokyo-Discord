package handlers

import (
	"github.com/hoshinonyaruko/gensokyo-discord/callapi"
)

func init() {
	callapi.RegisterHandler("send_msg_async", HandleSendMsg)
}
