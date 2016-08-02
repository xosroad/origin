package builds

import (
	"fmt"

	g "github.com/onsi/ginkgo"
	o "github.com/onsi/gomega"

	exutil "github.com/openshift/origin/test/extended/util"
)

var _ = g.Describe("[builds] build have source revision metadata", func() {
	defer g.GinkgoRecover()
	var (
		buildFixture = exutil.FixturePath("testdata", "test-build-revision.json")
		oc           = exutil.NewCLI("cli-build-revision", exutil.KubeConfigPath())
	)

	g.JustBeforeEach(func() {
		g.By("waiting for builder service account")
		err := exutil.WaitForBuilderAccount(oc.KubeREST().ServiceAccounts(oc.Namespace()))
		o.Expect(err).NotTo(o.HaveOccurred())
		oc.Run("create").Args("-f", buildFixture).Execute()
	})

	g.Describe("started build", func() {
		g.It("should contain source revision information", func() {
			g.By("starting the build with")
			out, err := oc.Run("start-build").Args("sample-build").Output()
			fmt.Fprintf(g.GinkgoWriter, "\nstart-build output:\n%s\n", out)
			o.Expect(err).NotTo(o.HaveOccurred())

			g.By("waiting for the build to complete")
			err = exutil.WaitForABuild(oc.REST().Builds(oc.Namespace()), "sample-build-1", exutil.CheckBuildSuccessFn, exutil.CheckBuildFailedFn)
			if err != nil {
				exutil.DumpBuildLogs("sample-build", oc)
			}
			o.Expect(err).NotTo(o.HaveOccurred())

			g.By(fmt.Sprintf("verifying the build %q status", "sample-build-1"))
			build, err := oc.REST().Builds(oc.Namespace()).Get("sample-build-1")
			o.Expect(err).NotTo(o.HaveOccurred())
			o.Expect(build.Spec.Revision).NotTo(o.BeNil())
			o.Expect(build.Spec.Revision.Git).NotTo(o.BeNil())
			o.Expect(build.Spec.Revision.Git.Commit).NotTo(o.BeEmpty())
			o.Expect(build.Spec.Revision.Git.Author.Name).NotTo(o.BeEmpty())
			o.Expect(build.Spec.Revision.Git.Committer.Name).NotTo(o.BeEmpty())
			o.Expect(build.Spec.Revision.Git.Message).NotTo(o.BeEmpty())
		})
	})
})
