/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2021 Nvidia
 *
 */

package steadyState

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"kubevirt.io/client-go/kubecli"
	"kubevirt.io/client-go/log"
	"kubevirt.io/kubevirt/tools/perfscale-load-generator/config"
	objUtil "kubevirt.io/kubevirt/tools/perfscale-load-generator/object"
	"kubevirt.io/kubevirt/tools/perfscale-load-generator/watcher"
)

type SteadyStateLoadGenerator struct {
	Done <-chan time.Time
}

type SteadyStateJob struct {
	Workload   *config.Workload
	virtClient kubecli.KubevirtClient
	UUID       string
	objType    string
	firstLoop  bool
	churn      int
}

// NewSteadyStateJob
func newSteadyStateJob(virtClient kubecli.KubevirtClient, workload *config.Workload) *SteadyStateJob {
	uid, _ := uuid.NewUUID()
	return &SteadyStateJob{
		virtClient: virtClient,
		Workload:   workload,
		firstLoop:  true,
		churn:      workload.Churn,
		UUID:       uid.String(),
	}
}

func (b *SteadyStateLoadGenerator) Delete(virtClient kubecli.KubevirtClient, workload *config.Workload) {
	ss := newSteadyStateJob(virtClient, workload)
	getObject, objType := objUtil.FindObject(virtClient, workload.Object, workload.Count)
	ss.objType = objType
	if getObject != nil {
		labels := getObject.GetLabels()
		jobUUID := labels[config.WorkloadLabel]
		log.Log.V(2).Infof("Deleting all workloads for job %s", jobUUID)
		objUtil.DeleteAllObjectsInNamespaces(virtClient, objType, config.GetListOpts(config.WorkloadLabel, jobUUID))
		ss.watchDelete(getObject)
	}
	log.Log.V(2).Infof("All workloads for job have been deleted")
	return
}

func (b *SteadyStateLoadGenerator) Run(virtClient kubecli.KubevirtClient, workload *config.Workload) {
	ss := newSteadyStateJob(virtClient, workload)
	for {
		select {
		case <-b.Done:
			log.Log.V(1).Infof("SteadyState Load Generator duration has timed out")
			return
		default:
		}
		ss.CreateWorkload()
		ss.Wait()
		ss.DeleteWorkload()
		ss.Wait()
		// Replace deleted objects
		ss.Workload.Count = ss.Workload.Churn
	}
}

// TODO: build helper function for createBurst so steady-state can call create burst or other types
//       of create during its create cycle
func (b *SteadyStateJob) CreateWorkload() {
	log.Log.V(1).Infof("SteadyState Load Generator CreateWorkload")

	var wg sync.WaitGroup
	obj := b.Workload.Object
	count := b.Workload.Count

	for replica := 1; replica <= count; replica++ {
		log.Log.V(2).Infof("Replica %d of %d", replica, count)
		templateData := objUtil.GenerateObjectTemplateData(obj, replica)

		newObject, err := objUtil.RenderObject(templateData, obj.ObjectTemplate)
		if err != nil {
			log.Log.Errorf("error rendering obj: %v", err)
		}
		config.AddLabels(newObject, b.UUID)
		if b.objType == "" {
			b.objType = objUtil.GetObjectResource(newObject)
		}

		if _, err := objUtil.CreateObject(b.virtClient, newObject); err != nil {
			log.Log.Errorf("error creating obj %s: %v", newObject.GroupVersionKind().Kind, err)
		}

		wg.Add(1)
		go func(newObject *unstructured.Unstructured) {
			defer wg.Done()
			b.watchCreate(newObject)
			log.Log.Infof("obj %s is available", newObject.GroupVersionKind().Kind)
		}(newObject)
	}
	wg.Wait()
}

func (b *SteadyStateJob) DeleteWorkload() {
	log.Log.V(1).Infof("SteadyState Load Generator DeleteWorkload")
	obj := b.Workload.Object
	count := b.Workload.Churn

	var wg sync.WaitGroup
	for replica := 1; replica <= count; replica++ {
		templateData := objUtil.GenerateObjectTemplateData(obj, replica)
		newObject, err := objUtil.RenderObject(templateData, obj.ObjectTemplate)
		if err != nil {
			log.Log.Errorf("error rendering obj: %v", err)
		}

		log.Log.V(3).Infof("Deleting obj %s", newObject.GetName())
		objUtil.DeleteObject(b.virtClient, *newObject, b.objType, 0)

		wg.Add(1)
		go func(newObject *unstructured.Unstructured) {
			defer wg.Done()
			b.watchDelete(newObject)
			log.Log.Infof("obj %s was deleted", newObject.GroupVersionKind().Kind)
		}(newObject)
	}
	wg.Wait()
}

func (b *SteadyStateJob) watchCreate(obj *unstructured.Unstructured) {
	count := b.Workload.Count
	objWatcher := watcher.NewObjListWatcher(
		b.virtClient,
		b.objType,
		count,
		*config.GetListOpts(config.WorkloadLabel, b.UUID))
	objWatcher.Run()
	log.Log.Infof("Wait for obj(s) %s to be available", b.objType)
	objWatcher.WaitRunning(b.Workload.Timeout.Duration)

	objWatcher.Stop()
}

func (b *SteadyStateJob) watchDelete(obj *unstructured.Unstructured) {
	count := b.Workload.Count
	// We expect b.churn fewer objects
	count = count - b.churn

	objWatcher := watcher.NewObjListWatcher(
		b.virtClient,
		b.objType,
		count,
		*config.GetListOpts(config.WorkloadLabel, b.UUID))
	objWatcher.Run()
	log.Log.Infof("Wait for obj(s) %s to be deleted", b.objType)
	objWatcher.WaitDeletion(b.Workload.Timeout.Duration)

	objWatcher.Stop()
}

func (b *SteadyStateJob) Wait() {
	log.Log.V(1).Infof("SteadyState Load Generator Wait")
	//TODO: use churn rate to calculate how long to sleep
	time.Sleep(20 * time.Second)
}
