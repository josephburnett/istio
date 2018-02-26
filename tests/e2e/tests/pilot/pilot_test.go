// Copyright 2018 Istio Authors
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

package pilot

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/davecgh/go-spew/spew"

	"istio.io/istio/pkg/log"
	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

const (
	authTestName   = "Auth"
	noAuthTestName = "NoAuth"
)

// AuthMode is an enumeration for the auth mode flag.
type authMode string

const (
	authModeEnable  authMode = "enable"
	authModeDisable authMode = "disable"
	authModeBoth    authMode = "both"
)

var (
	config = tutil.NewConfig()

	// Enable/disable auth, or run both for the tests.
	authmode string
	verbose  bool
)

func init() {
	flag.StringVar(&config.Hub, "hub", config.Hub, "Docker hub")
	flag.StringVar(&config.Tag, "tag", config.Tag, "Docker tag")
	flag.StringVar(&config.IstioNamespace, "ns", config.IstioNamespace,
		"Namespace in which to install Istio components (empty to create/delete temporary one)")
	flag.StringVar(&config.Namespace, "n", config.Namespace,
		"Namespace in which to install the applications (empty to create/delete temporary one)")
	flag.StringVar(&config.Registry, "registry", config.Registry, "Pilot registry")
	flag.BoolVar(&verbose, "verbose", false, "Debug level noise from proxies")
	flag.BoolVar(&config.CheckLogs, "logs", config.CheckLogs,
		"Validate pod logs (expensive in long-running tests)")

	flag.StringVar(&config.KubeConfig, "kubeconfig", config.KubeConfig,
		"kube config file (missing or empty file makes the test use in-cluster kube config instead)")
	flag.IntVar(&config.TestCount, "count", config.TestCount, "Number of times to run each test")
	flag.StringVar(&authmode, "auth", string(authModeBoth),
		fmt.Sprintf("Auth mode for the tests (Choose from %s, %s, %s)", authModeEnable, authModeDisable, authModeBoth))
	flag.BoolVar(&config.Mixer, "mixer", config.Mixer, "Enable / disable mixer.")
	flag.BoolVar(&config.V1alpha1, "v1alpha1", config.V1alpha1, "Enable / disable v1alpha1 routing rules.")
	flag.BoolVar(&config.V1alpha2, "v1alpha2", config.V1alpha2, "Enable / disable v1alpha2 routing rules.")
	flag.StringVar(&config.ErrorLogsDir, "errorlogsdir", config.ErrorLogsDir,
		"Store per pod logs as individual files in specific directory instead of writing to stderr.")
	flag.StringVar(&config.CoreFilesDir, "core-files-dir", config.CoreFilesDir,
		"Copy core files to this directory on the Kubernetes node machine.")

	// If specified, only run one test
	flag.StringVar(&config.SelectedTest, "testtype", config.SelectedTest,
		"Select test to run (default is all tests)")

	flag.BoolVar(&config.UseAutomaticInjection, "use-sidecar-injector", config.UseAutomaticInjection,
		"Use automatic sidecar injector")
	flag.BoolVar(&config.UseAdmissionWebhook, "use-admission-webhook", config.UseAdmissionWebhook,
		"Use k8s external admission webhook for config validation")

	flag.StringVar(&config.AdmissionServiceName, "admission-service-name", config.AdmissionServiceName,
		"Name of admission webhook service name")

	flag.IntVar(&config.DebugPort, "debugport", config.DebugPort, "Debugging port")

	flag.BoolVar(&config.DebugImagesAndMode, "debug", config.DebugImagesAndMode,
		"Use debug images and mode (false for prod)")
	flag.BoolVar(&config.SkipCleanup, "skip-cleanup", config.SkipCleanup,
		"Debug, skip clean up")
	flag.BoolVar(&config.SkipCleanupOnFailure, "skip-cleanup-on-failure", config.SkipCleanupOnFailure,
		"Debug, skip clean up on failure")
}

func setup(env *tutil.Environment, t *testing.T) {
	tutil.Tlog("Deploying infrastructure", spew.Sdump(env.Config))
	if env.Err = env.Setup(); env.Err != nil {
		t.Fatal(env.Err)
	}
}

func teardown(env *tutil.Environment) {
	env.Teardown()
}

func TestPilot(t *testing.T) {
	if verbose {
		config.Verbosity = 3
	}

	// Only run the tests if the user has defined the KUBECONFIG environment variable.
	if config.KubeConfig == "" {
		t.Skip("Env variable KUBECONFIG not set. Skipping tests")
	}

	if config.Hub == "" {
		t.Skip("HUB not specified. Skipping tests")
	}

	if config.Tag == "" {
		t.Skip("TAG not specified. Skipping tests")
	}

	if config.Namespace != "" && authMode(authmode) == authModeBoth {
		t.Skipf("When namespace(=%s) is specified, auth mode(=%s) must be one of enable or disable. Skipping tests.",
			config.Namespace, authmode)
	}

	noAuthConfig := config
	authConfig := config
	authConfig.Auth = true

	switch authMode(authmode) {
	case authModeEnable:
		doTest(authTestName, authConfig, t)
	case authModeDisable:
		doTest(noAuthTestName, noAuthConfig, t)
	case authModeBoth:
		doTest(noAuthTestName, noAuthConfig, t)
		doTest(authTestName, authConfig, t)
	default:
		t.Fatalf("Unknown auth mode(=%s).", authmode)
	}
}

func doTest(testName string, config *tutil.Config, t *testing.T) {
	t.Run(testName, func(t *testing.T) {
		env := tutil.NewEnvironment(*config)
		defer teardown(env)
		setup(env, t)

		tests := []tutil.Test{
			&http{Environment: env},
			&grpc{Environment: env},
			&tcp{Environment: env},
			&headless{Environment: env},
			&ingress{Environment: env},
			&egressRules{Environment: env},
			&routing{Environment: env},
			&routingToEgress{Environment: env},
			&zipkin{Environment: env},
			&authExclusion{Environment: env},
			&kubernetesExternalNameServices{Environment: env},
		}

		for _, test := range tests {
			// If the user has specified a test, skip all other tests
			if len(config.SelectedTest) > 0 && config.SelectedTest != test.String() {
				continue
			}

			// Run the test the configured number of times.
			for i := 0; i < config.TestCount; i++ {
				testName := test.String()
				if config.TestCount > 1 {
					testName = testName + "_attempt_" + strconv.Itoa(i+1)
				}
				t.Run(testName, func(t *testing.T) {
					if env.Err = test.Setup(); env.Err != nil {
						t.Fatal(env.Err)
					}
					defer test.Teardown()

					if env.Err = test.Run(); env.Err != nil {
						t.Error(env.Err)
					}
				})
			}
		}
	})
}

// TODO(nmittler): convert individual tests over to pure golang tests
func TestMain(m *testing.M) {
	flag.Parse()
	_ = log.Configure(log.NewOptions())

	// Run all tests.
	os.Exit(m.Run())
}
