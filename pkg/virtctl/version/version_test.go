package version_test

import (
	"fmt"
	goruntime "runtime"
	"time"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"kubevirt.io/client-go/kubecli"
	virt_version "kubevirt.io/client-go/version"
)

var _ = Describe("Version", func() {

	var ctrl *gomock.Controller

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		kubecli.GetKubevirtClientFromClientConfig = kubecli.GetMockKubevirtClientFromClientConfig
		kubecli.MockKubevirtClientInstance = kubecli.NewMockKubevirtClient(ctrl)
		serverVersionInterface := kubecli.NewMockServerVersionInterface(ctrl)
		kubecli.MockKubevirtClientInstance.EXPECT().ServerVersion().Return(serverVersionInterface).AnyTimes()
		serverVersionInterface.EXPECT().Get().Return(&virt_version.Info{
			GitVersion:   "v0.46.1",
			GitCommit:    "fda30004223b51f9e604276419a2b376652cb5ad",
			GitTreeState: "clear",
			BuildDate:    time.Now().Format("%Y-%m-%dT%H:%M:%SZ"),
			GoVersion:    goruntime.Version(),
			Compiler:     goruntime.Compiler,
			Platform:     fmt.Sprintf("%s/%s", goruntime.GOOS, goruntime.GOARCH),
		}, nil,
		).AnyTimes()
	})
})
