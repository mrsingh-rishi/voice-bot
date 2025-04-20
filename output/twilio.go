package output

import (
    "context"
    "fmt"
    "log"

    "github.com/gofiber/websocket/v2"
)

const EndOfUtterance = "__END_OF_UTTERANCE__"

type TwilioOutput struct {
    ctx                 context.Context
    cancel              context.CancelFunc
    OutputDeviceChannel <-chan string
    streamSid           string
    ws                  *websocket.Conn
}

func NewTwilioOutput(
    streamSid string,
    ws *websocket.Conn,
    outputDeviceChannel <-chan string,
) (*TwilioOutput, error) {
    if outputDeviceChannel == nil {
        return nil, fmt.Errorf("output device channel is required")
    }
    ctx, cancel := context.WithCancel(context.Background())
    return &TwilioOutput{
        ctx:                 ctx,
        cancel:              cancel,
        OutputDeviceChannel: outputDeviceChannel,
        streamSid:           streamSid,
        ws:                  ws,
    }, nil
}

func (o *TwilioOutput) Start() {
    go func() {
        for {
            select {
            case <-o.ctx.Done():
                return
            case payload, ok := <-o.OutputDeviceChannel:
                if !ok {
                    return
                }
                // if it's our end‑of‑utterance sentinel, send a mark
                if payload == EndOfUtterance {
                    o.sendMarkEvent()
                } else {
                    o.sendMediaEvent(payload)
                }
            }
        }
    }()
}

func (o *TwilioOutput) sendMediaEvent(payload string) {
    mediaMsg := map[string]interface{}{
        "event":     "media",
        "streamSid": o.streamSid,
        "media": map[string]string{
            "payload": payload,
        },
    }
    if err := o.ws.WriteJSON(mediaMsg); err != nil {
        log.Printf("TwilioOutput media write error: %v", err)
    }
}

func (o *TwilioOutput) sendMarkEvent() {
    markMsg := map[string]interface{}{
        "event":     "mark",
        "streamSid": o.streamSid,
        "mark": map[string]string{
            "name": "audio chunks sent",
        },
    }
    if err := o.ws.WriteJSON(markMsg); err != nil {
        log.Printf("TwilioOutput mark write error: %v", err)
    }
}

func (o *TwilioOutput) Stop() {
    o.cancel()
    if o.ws != nil {
        o.ws.Close()
    }
}
