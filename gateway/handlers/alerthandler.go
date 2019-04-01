// Copyright (c) Alex Ellis 2017. All rights reserved.
// Licensed under the MIT license. See LICENSE file in the project root for full license information.

package handlers

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"fmt"

	"github.com/openfaas/faas/gateway/requests"
	"math"
)

const (
	// DefaultMinReplicas is the minimal amount of replicas for a service.
	DefaultMinReplicas = 1

	// DefaultMaxReplicas is the amount of replicas a service will auto-scale up to.
	DefaultMaxReplicas = 20

	// DefaultScalingFactor is the defining proportion for the scaling increments.
	DefaultScalingFactor = 20

	// MinScaleLabel label indicating min scale for a function
	MinScaleLabel = "com.openfaas.scale.min"

	// MaxScaleLabel label indicating max scale for a function
	MaxScaleLabel = "com.openfaas.scale.max"

	// ScalingFactorLabel label indicates the scaling factor for a function
	ScalingFactorLabel = "com.openfaas.scale.factor"
)

// MakeAlertHandler handles alerts from Prometheus Alertmanager
func MakeAlertHandler(service ServiceQuery) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		log.Println("Alert received.")

		body, readErr := ioutil.ReadAll(r.Body)

		log.Println(string(body))

		if readErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Unable to read alert."))

			log.Println(readErr)
			return
		}

		var req requests.PrometheusAlert
		err := json.Unmarshal(body, &req)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Unable to parse alert, bad format."))
			log.Println(err)
			return
		}

		errors := handleAlerts(&req, service)
		if len(errors) > 0 {
			log.Println(errors)
			var errorOutput string
			for d, err := range errors {
				errorOutput += fmt.Sprintf("[%d] %s\n", d, err)
			}
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(errorOutput))
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func handleAlerts(req *requests.PrometheusAlert, service ServiceQuery) []error {
	var errors []error
	for _, alert := range req.Alerts {
		if err := scaleService(alert, service); err != nil {
			log.Println(err)
			errors = append(errors, err)
		}
	}

	return errors
}

//modified by Xavier，modify scale policy
func scaleService(alert requests.PrometheusInnerAlert, service ServiceQuery) error {
	var err error
	serviceName := alert.Labels.FunctionName

	if len(serviceName) > 0 {
		queryResponse, getErr := service.GetReplicas(serviceName)
		if getErr == nil {
			//status := alert.Status

			newReplicas := CalculateReplicas(&alert, queryResponse.Replicas, uint64(queryResponse.MaxReplicas), queryResponse.MinReplicas, queryResponse.ScalingFactor)

			log.Printf("[Scale] function=%s %d => %d.\n", serviceName, queryResponse.Replicas, newReplicas)
			if newReplicas == queryResponse.Replicas {
				return nil
			}

			updateErr := service.SetReplicas(serviceName, newReplicas)
			if updateErr != nil {
				err = updateErr
			}
		}
	}
	return err
}

// CalculateReplicas decides what replica count to set depending on current/desired amount
func CalculateReplicas(alert *requests.PrometheusInnerAlert, currentReplicas uint64, maxReplicas uint64, minReplicas uint64, scalingFactor uint64) uint64 {
	newReplicas := currentReplicas
	//扩容步长
	scaleStep := uint64(math.Ceil(float64(maxReplicas) / 100 * float64(scalingFactor)))
	//缩容步长
	downStep := 1
	status := alert.Status
	if status == "firing" {
		//判断是扩容还是缩容
		if alert.Labels.Scale == "scale" && scaleStep > 0{
			//扩容策略
			if currentReplicas + scaleStep > maxReplicas {
				newReplicas = maxReplicas
			} else {
				newReplicas = currentReplicas + scaleStep
			}
		}
		if alert.Labels.Scale == "down" && currentReplicas > 1{
			//缩容策略
			newReplicas = currentReplicas - uint64(downStep)
		}
	}
	//step := uint64((float64(maxReplicas) / 100) * float64(scalingFactor))

	//if status == "firing" {
	//	if currentReplicas == 1 {
	//		newReplicas = step
	//	} else {
	//		if currentReplicas+step > maxReplicas {
	//			newReplicas = maxReplicas
	//		} else {
	//			newReplicas = currentReplicas + step
	//		}
	//	}
	//} else { // Resolved event.
	//	newReplicas = minReplicas
	//}

	return newReplicas
}
