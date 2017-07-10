// Copyright (C) 2017-Present Pivotal Software, Inc. All rights reserved.
//
// This program and the accompanying materials are made available under
// the terms of the under the Apache License, Version 2.0 (the "License”);
// you may not use this file except in compliance with the License.
//
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.  See the
// License for the specific language governing permissions and limitations
// under the License.

package lifecycle_test

import (
	"bpm/bpm"
	"bpm/models"
	"bpm/runc/client"
	"bpm/runc/lifecycle"
	"bpm/runc/lifecycle/lifecyclefakes"
	"bpm/usertools"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	"code.cloudfoundry.org/lager/lagertest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

var _ = Describe("RuncJobLifecycle", func() {
	var (
		fakeRuncAdapter *lifecyclefakes.FakeRuncAdapter
		fakeRuncClient  *lifecyclefakes.FakeRuncClient
		fakeUserFinder  *lifecyclefakes.FakeUserFinder

		logger *lagertest.TestLogger

		jobConfig *bpm.Config
		jobSpec   specs.Spec

		expectedJobName,
		expectedProcName,
		expectedContainerID,
		expectedSystemRoot,
		expectedPidDir string

		expectedStdout, expectedStderr *os.File

		expectedUser specs.User

		fakeClock *fakeclock.FakeClock

		runcLifecycle *lifecycle.RuncLifecycle
	)

	BeforeEach(func() {
		fakeClock = fakeclock.NewFakeClock(time.Now())
		fakeRuncAdapter = &lifecyclefakes.FakeRuncAdapter{}
		fakeRuncClient = &lifecyclefakes.FakeRuncClient{}
		fakeUserFinder = &lifecyclefakes.FakeUserFinder{}

		logger = lagertest.NewTestLogger("lifecycle")

		expectedUser = specs.User{Username: "vcap", UID: 300, GID: 400}
		fakeUserFinder.LookupReturns(expectedUser, nil)

		var err error
		expectedPidDir = "a-pid-dir"
		expectedStdout, err = ioutil.TempFile("", "runc-lifecycle-stdout")
		Expect(err).NotTo(HaveOccurred())
		expectedStderr, err = ioutil.TempFile("", "runc-lifecycle-stderr")
		Expect(err).NotTo(HaveOccurred())
		fakeRuncAdapter.CreateJobPrerequisitesReturns(expectedPidDir, expectedStdout, expectedStderr, nil)

		expectedJobName = "example"
		expectedProcName = "server"
		expectedContainerID = fmt.Sprintf("%s-%s", expectedJobName, expectedProcName)

		jobConfig = &bpm.Config{
			Name:       expectedProcName,
			Executable: "/bin/sleep",
		}
		jobSpec = specs.Spec{
			Version: "example-version",
		}
		fakeRuncAdapter.BuildSpecReturns(jobSpec, nil)

		expectedSystemRoot = "system-root"

		runcLifecycle = lifecycle.NewRuncLifecycle(
			fakeRuncClient,
			fakeRuncAdapter,
			fakeUserFinder,
			fakeClock,
			expectedSystemRoot,
		)
	})

	AfterEach(func() {
		Expect(os.RemoveAll(expectedStdout.Name())).To(Succeed())
		Expect(os.RemoveAll(expectedStderr.Name())).To(Succeed())
	})

	Describe("StartJob", func() {
		It("builds the runc spec, bundle, and runs the container", func() {
			err := runcLifecycle.StartJob(expectedJobName, jobConfig)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeUserFinder.LookupCallCount()).To(Equal(1))
			Expect(fakeUserFinder.LookupArgsForCall(0)).To(Equal(usertools.VcapUser))

			Expect(fakeRuncAdapter.CreateJobPrerequisitesCallCount()).To(Equal(1))
			systemRoot, jobName, cfg, user := fakeRuncAdapter.CreateJobPrerequisitesArgsForCall(0)
			Expect(systemRoot).To(Equal(expectedSystemRoot))
			Expect(jobName).To(Equal(expectedJobName))
			Expect(cfg).To(Equal(jobConfig))
			Expect(user).To(Equal(expectedUser))

			Expect(fakeRuncAdapter.BuildSpecCallCount()).To(Equal(1))
			systemRoot, jobName, cfg, user = fakeRuncAdapter.BuildSpecArgsForCall(0)
			Expect(systemRoot).To(Equal(expectedSystemRoot))
			Expect(jobName).To(Equal(expectedJobName))
			Expect(cfg).To(Equal(jobConfig))
			Expect(user).To(Equal(expectedUser))

			Expect(fakeRuncClient.CreateBundleCallCount()).To(Equal(1))
			bundlePath, spec, user := fakeRuncClient.CreateBundleArgsForCall(0)
			Expect(bundlePath).To(Equal(filepath.Join(expectedSystemRoot, "data", "bpm", "bundles", expectedJobName, expectedProcName)))
			Expect(spec).To(Equal(jobSpec))
			Expect(user).To(Equal(expectedUser))

			Expect(fakeRuncClient.RunContainerCallCount()).To(Equal(1))
			pidFilePath, bundlePath, cid, stdout, stderr := fakeRuncClient.RunContainerArgsForCall(0)
			Expect(pidFilePath).To(Equal(filepath.Join(expectedPidDir, fmt.Sprintf("%s.pid", expectedProcName))))
			Expect(bundlePath).To(Equal(filepath.Join(expectedSystemRoot, "data", "bpm", "bundles", expectedJobName, expectedProcName)))
			Expect(cid).To(Equal(expectedContainerID))
			Expect(stdout).To(Equal(expectedStdout))
			Expect(stderr).To(Equal(expectedStderr))
		})

		Context("when looking up the vcap user fails", func() {
			BeforeEach(func() {
				fakeUserFinder.LookupReturns(specs.User{}, errors.New("boom"))
			})

			It("returns an error", func() {
				err := runcLifecycle.StartJob(expectedJobName, jobConfig)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when creating the system files fails", func() {
			BeforeEach(func() {
				fakeRuncAdapter.CreateJobPrerequisitesReturns("", nil, nil, errors.New("boom"))
			})

			It("returns an error", func() {
				err := runcLifecycle.StartJob(expectedJobName, jobConfig)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when building the runc spec fails", func() {
			BeforeEach(func() {
				fakeRuncAdapter.BuildSpecReturns(specs.Spec{}, errors.New("boom"))
			})

			It("returns an error", func() {
				err := runcLifecycle.StartJob(expectedJobName, jobConfig)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when building the bundle fails", func() {
			BeforeEach(func() {
				fakeRuncClient.CreateBundleReturns(errors.New("boom!"))
			})

			It("returns an error", func() {
				err := runcLifecycle.StartJob(expectedJobName, jobConfig)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when running the container fails", func() {
			BeforeEach(func() {
				fakeRuncClient.RunContainerReturns(errors.New("boom!"))
			})

			It("returns an error", func() {
				err := runcLifecycle.StartJob(expectedJobName, jobConfig)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("StopJob", func() {
		var exitTimeout time.Duration

		BeforeEach(func() {
			exitTimeout = 5 * time.Second

			fakeRuncClient.ContainerStateReturns(specs.State{
				Status: "stopped",
			}, nil)
		})

		It("stops the container", func() {
			err := runcLifecycle.StopJob(logger, expectedJobName, jobConfig, exitTimeout)
			Expect(err).NotTo(HaveOccurred())

			Eventually(fakeRuncClient.SignalContainerCallCount).Should(Equal(1))
			cid, signal := fakeRuncClient.SignalContainerArgsForCall(0)
			Expect(cid).To(Equal(expectedContainerID))
			Expect(signal).To(Equal(client.Term))

			Expect(fakeRuncClient.ContainerStateCallCount()).To(Equal(1))
			cid = fakeRuncClient.ContainerStateArgsForCall(0)
			Expect(cid).To(Equal(expectedContainerID))
		})

		Context("when the container does not stop immediately", func() {
			var stopped chan struct{}
			BeforeEach(func() {
				stopped = make(chan struct{})

				fakeRuncClient.ContainerStateStub = func(containerID string) (specs.State, error) {
					select {
					case <-stopped:
						return specs.State{Status: "stopped"}, nil
					default:
						return specs.State{Status: "running"}, nil
					}
				}
			})

			It("polls the container state every second until it stops", func() {
				errChan := make(chan error)
				go func() {
					defer GinkgoRecover()
					errChan <- runcLifecycle.StopJob(logger, expectedJobName, jobConfig, exitTimeout)
				}()

				Eventually(fakeRuncClient.SignalContainerCallCount).Should(Equal(1))
				cid, signal := fakeRuncClient.SignalContainerArgsForCall(0)
				Expect(cid).To(Equal(expectedContainerID))
				Expect(signal).To(Equal(client.Term))

				Eventually(fakeRuncClient.ContainerStateCallCount).Should(Equal(1))
				Expect(fakeRuncClient.ContainerStateArgsForCall(0)).To(Equal(expectedContainerID))

				fakeClock.WaitForWatcherAndIncrement(1 * time.Second)

				Eventually(fakeRuncClient.ContainerStateCallCount).Should(Equal(2))
				Expect(fakeRuncClient.ContainerStateArgsForCall(1)).To(Equal(expectedContainerID))

				close(stopped)
				fakeClock.WaitForWatcherAndIncrement(1 * time.Second)

				Eventually(fakeRuncClient.ContainerStateCallCount).Should(Equal(3))
				Expect(fakeRuncClient.ContainerStateArgsForCall(2)).To(Equal(expectedContainerID))

				Eventually(errChan).Should(Receive(BeNil()), "stop job did not exit in time")
			})

			Context("and the exit timeout has passed", func() {
				It("sends a SIGQUIT and returns a timeout error", func() {
					errChan := make(chan error)
					go func() {
						defer GinkgoRecover()
						errChan <- runcLifecycle.StopJob(logger, expectedJobName, jobConfig, exitTimeout)
					}()

					Eventually(fakeRuncClient.SignalContainerCallCount).Should(Equal(1))
					cid, signal := fakeRuncClient.SignalContainerArgsForCall(0)
					Expect(cid).To(Equal(expectedContainerID))
					Expect(signal).To(Equal(client.Term))

					Eventually(fakeRuncClient.ContainerStateCallCount).Should(Equal(1))
					Expect(fakeRuncClient.ContainerStateArgsForCall(0)).To(Equal(expectedContainerID))

					fakeClock.WaitForWatcherAndIncrement(1 * time.Second)

					Eventually(fakeRuncClient.ContainerStateCallCount).Should(Equal(2))
					Expect(fakeRuncClient.ContainerStateArgsForCall(1)).To(Equal(expectedContainerID))

					fakeClock.WaitForWatcherAndIncrement(exitTimeout)

					var actualError error
					Eventually(errChan).Should(Receive(&actualError))
					Expect(actualError).To(Equal(lifecycle.TimeoutError))

					Expect(fakeRuncClient.SignalContainerCallCount()).Should(Equal(2))
					cid, signal = fakeRuncClient.SignalContainerArgsForCall(1)
					Expect(cid).To(Equal(expectedContainerID))
					Expect(signal).To(Equal(client.Quit))
				})
			})
		})

		Context("when fetching the container state fails", func() {
			BeforeEach(func() {
				fakeRuncClient.ContainerStateReturns(specs.State{}, errors.New("boom"))
			})

			It("keeps attempting to fetch the state", func() {
				errChan := make(chan error)
				go func() {
					defer GinkgoRecover()
					errChan <- runcLifecycle.StopJob(logger, expectedJobName, jobConfig, exitTimeout)
				}()

				Eventually(fakeRuncClient.ContainerStateCallCount).Should(Equal(1))
				Expect(fakeRuncClient.ContainerStateArgsForCall(0)).To(Equal(expectedContainerID))

				fakeClock.WaitForWatcherAndIncrement(1 * time.Second)

				Eventually(fakeRuncClient.ContainerStateCallCount).Should(Equal(2))
				Expect(fakeRuncClient.ContainerStateArgsForCall(1)).To(Equal(expectedContainerID))

				fakeClock.WaitForWatcherAndIncrement(1 * time.Second)

				Eventually(fakeRuncClient.ContainerStateCallCount).Should(Equal(3))
				Expect(fakeRuncClient.ContainerStateArgsForCall(2)).To(Equal(expectedContainerID))

				fakeClock.WaitForWatcherAndIncrement(exitTimeout)

				var actualError error
				Eventually(errChan).Should(Receive(&actualError))
				Expect(actualError).To(Equal(lifecycle.TimeoutError))
			})
		})

		Context("when stopping a container fails", func() {
			var expectedErr error

			BeforeEach(func() {
				expectedErr = errors.New("an error")
				fakeRuncClient.SignalContainerReturns(expectedErr)
			})

			It("returns an error", func() {
				err := runcLifecycle.StopJob(logger, expectedJobName, jobConfig, exitTimeout)
				Expect(err).To(Equal(expectedErr))
			})
		})
	})

	Describe("RemoveJob", func() {
		It("deletes the container", func() {
			err := runcLifecycle.RemoveJob(expectedJobName, jobConfig)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeRuncClient.DeleteContainerCallCount()).To(Equal(1))
			containerID := fakeRuncClient.DeleteContainerArgsForCall(0)
			Expect(containerID).To(Equal(expectedContainerID))
		})

		It("destroys the bundle", func() {
			err := runcLifecycle.RemoveJob(expectedJobName, jobConfig)
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeRuncClient.DestroyBundleCallCount()).To(Equal(1))
			bundlePath := fakeRuncClient.DestroyBundleArgsForCall(0)
			Expect(bundlePath).To(Equal(filepath.Join(expectedSystemRoot, "data", "bpm", "bundles", expectedJobName, expectedProcName)))
		})

		Context("when deleting a container fails", func() {
			var expectedErr error

			BeforeEach(func() {
				expectedErr = errors.New("an error")
				fakeRuncClient.DeleteContainerReturns(expectedErr)
			})

			It("returns an error", func() {
				err := runcLifecycle.RemoveJob(expectedJobName, jobConfig)
				Expect(err).To(Equal(expectedErr))
			})
		})

		Context("when destroying a bundle fails", func() {
			It("returns an error", func() {
				expectedErr := errors.New("an error2")
				fakeRuncClient.DestroyBundleReturns(expectedErr)
				err := runcLifecycle.RemoveJob(expectedJobName, jobConfig)
				Expect(err).To(Equal(expectedErr))
			})
		})
	})

	Describe("ListJobs", func() {
		It("calls the runc client", func() {
			_, err := runcLifecycle.ListJobs()
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeRuncClient.ListContainersCallCount()).To(Equal(1))
		})

		It("returns a list of bpm jobs", func() {
			containerStates := []client.ContainerState{
				{
					ID:             "job-process-2",
					InitProcessPid: 23456,
					Status:         "created",
				},
				{
					ID:             "job-process-1",
					InitProcessPid: 34567,
					Status:         "running",
				},
			}
			fakeRuncClient.ListContainersReturns(containerStates, nil)

			bpmJobs, err := runcLifecycle.ListJobs()
			Expect(err).NotTo(HaveOccurred())

			Expect(bpmJobs).To(ConsistOf([]models.Job{
				{Name: "job-process-2", Pid: 23456, Status: "created"},
				{Name: "job-process-1", Pid: 34567, Status: "running"},
			}))
		})

		Context("when listing jobs fails", func() {
			It("returns an error", func() {
				expectedErr := errors.New("list jobs error")
				fakeRuncClient.ListContainersReturns([]client.ContainerState{}, expectedErr)

				_, err := runcLifecycle.ListJobs()
				Expect(err).To(Equal(expectedErr))
			})
		})
	})

	Describe("GetJob", func() {
		BeforeEach(func() {
			fakeRuncClient.ContainerStateReturns(specs.State{ID: expectedContainerID, Pid: 1234, Status: "running"}, nil)
		})

		It("fetches the container state and translates it into a job", func() {
			job, err := runcLifecycle.GetJob(expectedJobName, jobConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(fakeRuncClient.ContainerStateCallCount()).To(Equal(1))
			Expect(fakeRuncClient.ContainerStateArgsForCall(0)).To(Equal(expectedContainerID))
			Expect(job).To(Equal(models.Job{
				Name:   expectedContainerID,
				Pid:    1234,
				Status: "running",
			}))
		})

		Context("when fetching the container state fails", func() {
			BeforeEach(func() {
				fakeRuncClient.ContainerStateReturns(specs.State{}, errors.New("boom!"))
			})

			It("returns an error", func() {
				_, err := runcLifecycle.GetJob(expectedJobName, jobConfig)
				Expect(err).To(HaveOccurred())
			})
		})
	})
})