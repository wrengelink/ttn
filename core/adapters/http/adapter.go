// Copyright © 2015 The Things Network
// Use of this source code is governed by the MIT license that can be found in the LICENSE file.

package http

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/TheThingsNetwork/ttn/core"
	"github.com/apex/log"
)

var ErrInvalidPort = fmt.Errorf("The given port is invalid")
var ErrInvalidPacket = fmt.Errorf("The given packet is invalid")

type Adapter struct {
	Ctx log.Interface
}

// NewAdapter constructs and allocate a new Broker <-> Handler http adapter
func NewAdapter(ctx log.Interface) (*Adapter, error) {
	return &Adapter{
		Ctx: ctx,
	}, nil
}

// Send implements the core.Adapter interface
func (a *Adapter) Send(p core.Packet, r ...core.Recipient) (core.Packet, error) {
	// Generate payload from core packet
	m, err := json.Marshal(p.Metadata)
	if err != nil {
		return core.Packet{}, ErrInvalidPacket
	}
	pl, err := p.Payload.MarshalBinary()
	if err != nil {
		return core.Packet{}, ErrInvalidPacket
	}
	payload := fmt.Sprintf(`{"payload":"%s","metadata":%s}`, base64.StdEncoding.EncodeToString(pl), m)

	// Prepare ground for parrallel http request
	nb := len(r)
	cherr := make(chan error, nb)
	chresp := make(chan core.Packet, nb)
	wg := sync.WaitGroup{}
	wg.Add(nb)

	// Run each request
	for _, recipient := range r {
		go func(recipient core.Recipient) {
			defer wg.Done()
			a.Ctx.WithField("recipient", recipient).Debug("POST Request")
			buf := new(bytes.Buffer)
			buf.Write([]byte(payload))

			// Send request
			resp, err := http.Post(fmt.Sprintf("http://%s", recipient.Address.(string)), "application/json", buf)
			if err != nil {
				cherr <- err
				return
			}

			// Check response code
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusCreated {
				cherr <- fmt.Errorf("Unexpected response from server: %s (%d)", resp.Status, resp.StatusCode)
				return
			}

			// Process response body
			raw := make([]byte, resp.ContentLength)
			n, err := resp.Body.Read(raw)
			defer resp.Body.Close()
			if err != nil && err != io.EOF {
				cherr <- err
				return
			}

			// Process packet
			var packet core.Packet
			if err := json.Unmarshal(raw[:n], &packet); err != nil {
				cherr <- err
				return
			}

			chresp <- packet
		}(recipient)
	}

	// Wait for each request to be done, and return
	wg.Wait()

	// Collect errors
	var errors []error
	for i := 0; i < len(cherr); i += 1 {
		errors = append(errors, <-cherr)
	}

	// Check responses
	if len(chresp) > 1 {
		return core.Packet{}, fmt.Errorf("Several positive answer from servers")
	}

	// Get packet
	select {
	case packet := <-chresp:
		return packet, nil
	default:
		return core.Packet{}, fmt.Errorf("No response packet available")
	}

	// Return Errors
	if errors != nil {
		return core.Packet{}, fmt.Errorf("Errors: %v", errors)
	}

	return core.Packet{}, fmt.Errorf("Unexpected error")
}

// Next implements the core.Adapter interface
func (a *Adapter) Next() (core.Packet, core.AckNacker, error) {
	// NOTE not implemented
	return core.Packet{}, nil, nil
}

// NextRegistration implements the core.Adapter interface
func (a *Adapter) NextRegistration() (core.Packet, core.AckNacker, error) {
	return core.Packet{}, nil, nil
}
