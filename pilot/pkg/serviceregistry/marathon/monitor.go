// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package marathon

import (
	"reflect"
	"sort"
	"time"

	"github.com/gambol99/go-marathon"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

type marathonServices []marathon.Application
type marathonServiceInstances []marathon.Task

// Monitor handles service and instance changes
type Monitor interface {
	Start(<-chan struct{})
	AppendServiceHandler(ServiceHandler)
	AppendInstanceHandler(InstanceHandler)
}

// InstanceHandler processes service instance change events
type InstanceHandler func(instance *marathon.Task, event model.Event) error

// ServiceHandler processes service change events
type ServiceHandler func(instances []*marathon.Application, event model.Event) error

type marathonMonitor struct {
	discovery            marathon.Marathon
	instanceCachedRecord marathonServiceInstances
	serviceCachedRecord  marathonServices
	instanceHandlers     []InstanceHandler
	serviceHandlers      []ServiceHandler
	period               time.Duration
}

// NewMarathonMonitor polls for changes in Consul Services and CatalogServices
func NewMarathonMonitor(addr string, period time.Duration) Monitor{
	client,_:= NewMarathonClient(addr)
	return &marathonMonitor{
		discovery:            client,
		period:               period,
		instanceCachedRecord: make(marathonServiceInstances, 0),
		serviceCachedRecord:  make(marathonServices,0),
		instanceHandlers:     make([]InstanceHandler, 0),
		serviceHandlers:      make([]ServiceHandler, 0),
	}

}

func (m *marathonMonitor) Start(stop <-chan struct{}) {
	m.run(stop)
}

func (m *marathonMonitor) run(stop <-chan struct{}) {
	ticker := time.NewTicker(m.period)
	for {
		select {
		case <-stop:
			ticker.Stop()
			return
		case <-ticker.C:
			m.updateServiceRecord()
			m.updateInstanceRecord()
		}
	}
}

func (m *marathonMonitor) updateServiceRecord() {
	svcs,err:=m.discovery.Applications(nil)
	if err != nil {
		log.Warnf("Could not fetch services: %v", err)
		return
	}
	//todo >>>map

	newRecord := marathonServices(svcs.Apps)
	if !reflect.DeepEqual(newRecord, m.serviceCachedRecord) {
		// This is only a work-around solution currently
		// Since Handler functions generally act as a refresher
		// regardless of the input, thus passing in meaningless
		// input should make functionalities work
		//TODO
		obj := []*marathon.Application{}
		var event model.Event
		for _, f := range m.serviceHandlers {
			go func(handler ServiceHandler) {
				if err := handler(obj, event); err != nil {
					log.Warnf("Error executing service handler function: %v", err)
				}
			}(f)
		}
		m.serviceCachedRecord = newRecord
	}
}

func (m *marathonMonitor) updateInstanceRecord() {
	svcs, err := m.discovery.Applications(nil)
	if err != nil {
		log.Warnf("Could not fetch instances: %v", err)
		return
	}

	//未按照consul,排序比较前后数据，故每次请求大概率不一样，即为默认变化更新
	//
	//sort.Strings(*(svcs.Apps[0].Tasks))
	//

	instances := make([]marathon.Task, 0)
	for _,app := range svcs.Apps {
		endpoints, err := m.discovery.Application(app.ID)
		if err != nil {
			log.Warnf("Could not retrieve service catalogue from consul: %v", err)
			continue
		}
		for _,tinstance := range endpoints.Tasks{
			instances = append(instances, *tinstance)
		}

	}

	newRecord := marathonServiceInstances(instances)
	sort.Sort(newRecord)
	if !reflect.DeepEqual(newRecord, m.instanceCachedRecord) {
		// This is only a work-around solution currently
		// Since Handler functions generally act as a refresher
		// regardless of the input, thus passing in meaningless
		// input should make functionalities work
		// TODO
		obj := []marathon.Application{}
		var event model.Event
		for _, f := range m.instanceHandlers {
			go func(handler InstanceHandler) {
				if err := handler(obj, event); err != nil {
					log.Warnf("Error executing instance handler function: %v", err)
				}
			}(f)
		}
		m.instanceCachedRecord = newRecord
	}
}

func (m *marathonMonitor) AppendServiceHandler(h ServiceHandler) {
	m.serviceHandlers = append(m.serviceHandlers, h)
}

func (m *marathonMonitor) AppendInstanceHandler(h InstanceHandler) {
	m.instanceHandlers = append(m.instanceHandlers, h)
}

// Len of the array
func (a marathonServiceInstances) Len() int {
	return len(a)
}

// Swap i and j
func (a marathonServiceInstances) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

// Less i and j
func (a marathonServiceInstances) Less(i, j int) bool {
	return a[i].ID < a[j].ID
}
