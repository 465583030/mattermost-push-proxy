// Copyright (c) 2015 Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package server

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/anachronistic/apns"
	"github.com/braintree/manners"
	"github.com/gorilla/mux"
	"gopkg.in/throttled/throttled.v1"
	throttledStore "gopkg.in/throttled/throttled.v1/store"
)

const (
	HEADER_FORWARDED = "X-Forwarded-For"
	HEADER_REAL_IP   = "X-Real-IP"
)

func Start() {
	LogInfo("Push proxy server is initializing...")

	router := mux.NewRouter()
	var handler http.Handler = router
	vary := throttled.VaryBy{}
	vary.RemoteAddr = false
	vary.Headers = strings.Fields(CfgPP.ThrottleVaryByHeader)
	th := throttled.RateLimit(throttled.PerSec(CfgPP.ThrottlePerSec), &vary, throttledStore.NewMemStore(CfgPP.ThrottleMemoryStoreSize))

	th.DeniedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		LogError(fmt.Sprintf("%v: code=429 ip=%v", r.URL.Path, GetIpAddress(r)))
		throttled.DefaultDeniedHandler.ServeHTTP(w, r)
	})

	handler = th.Throttle(router)

	r := router.PathPrefix("/api/v1").Subrouter()
	r.HandleFunc("/send_push", handleSendNotification).Methods("POST")

	go func() {
		err := manners.ListenAndServe(CfgPP.ListenAddress, handler)
		if err != nil {
			LogCritical(err.Error())
		}
	}()

	LogInfo("Server is listening on " + CfgPP.ListenAddress)
}

func Stop() {
	LogInfo("Stopping Server...")
	manners.Close()
}

func handleSendNotification(w http.ResponseWriter, r *http.Request) {
	msg := PushNotificationFromJson(r.Body)

	if msg == nil {
		LogError("Failed to read message body")
	}

	if len(msg.ServerId) == 0 {
		LogError("Failed because of missing server Id")
	}

	if msg.Platform == PUSH_NOTIFY_APPLE {
		go sendAppleNotification(msg)
	} else {
		LogError("Missing platform property")
	}
}

func sendAppleNotification(msg *PushNotification) {
	payload := apns.NewPayload()
	payload.Alert = msg.Message
	payload.Badge = msg.Badge

	pn := apns.NewPushNotification()
	pn.DeviceToken = msg.DeviceId
	pn.AddPayload(payload)
	client := apns.BareClient(CfgPP.ApplePushServer, CfgPP.ApplePushCertPublic, CfgPP.ApplePushCertPrivate)
	resp := client.Send(pn)

	if resp.Error != nil {
		LogError(fmt.Sprintf("Failed to send apple push sid=%v did=%v err=%v", msg.ServerId, msg.DeviceId, resp.Error))
	}
}

func LogInfo(msg string) {
	Log("INFO", msg)
}

func LogError(msg string) {
	Log("ERROR", msg)
}

func LogCritical(msg string) {
	Log("CRIT", msg)
	panic(msg)
}

func Log(level string, msg string) {
	log.Printf("%v %v\n", level, msg)
}

func GetIpAddress(r *http.Request) string {
	address := r.Header.Get(HEADER_FORWARDED)

	if len(address) == 0 {
		address = r.Header.Get(HEADER_REAL_IP)
	}

	if len(address) == 0 {
		address, _, _ = net.SplitHostPort(r.RemoteAddr)
	}

	return address
}
