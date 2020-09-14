package quickstarter

import (
	"bytes"
	b64 "encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/opendevstack/ods-core/tests/utils"
)

// TestQuickstarter tests given quickstarters. It expects a "steps.yml" file in
// a "testdata" directory within each quickstarter to test.
// The quickstarter(s) to run the tests for can be given as the last command
// line argument.
// If the argument starts with "." or "/", it is assumed to be a path to a
// folder - otherwise a folder next to "ods-core" is assumed, by default
// "ods-quickstarters". If the argument ends with "...", all directories with a
// "testdata" directory are tested, otherwise only the given folder is run.
func TestQuickstarter(t *testing.T) {

	var quickstarterPaths []string
	odsCoreRootPath := "../.."
	target := os.Args[len(os.Args)-1]
	if strings.HasPrefix(target, ".") || strings.HasPrefix(target, "/") {
		if strings.HasSuffix(target, "...") {
			quickstarterPaths = collectTestableQuickstarters(
				t, strings.TrimSuffix(target, "/..."),
			)
		} else {
			quickstarterPaths = append(quickstarterPaths, target)
		}
	} else {
		// No slash = quickstarter in ods-quickstarters
		// Ending with ... = all quickstarters in given folder
		// otherwise = exactly one quickstarter
		if !strings.Contains(target, "/") {
			quickstarterPaths = []string{fmt.Sprintf("%s/../%s/%s", odsCoreRootPath, "ods-quickstarters", target)}
		} else if strings.HasSuffix(target, "...") {
			quickstarterPaths = collectTestableQuickstarters(
				t, fmt.Sprintf("%s/../%s", odsCoreRootPath, strings.TrimSuffix(target, "/...")),
			)
		} else {
			quickstarterPaths = []string{fmt.Sprintf("%s/../%s", odsCoreRootPath, target)}
		}
	}

	config, err := utils.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	cdUserPassword, err := b64.StdEncoding.DecodeString(config["CD_USER_PWD_B64"])
	if err != nil {
		t.Fatalf("Error decoding cd_user password: %s", err)
	}

	fmt.Printf("Running test steps found in following directories:\n")
	for _, quickstarterPath := range quickstarterPaths {
		fmt.Printf("- %s\n", quickstarterPath)
	}
	for _, quickstarterPath := range quickstarterPaths {
		testdataPath := fmt.Sprintf("%s/testdata", quickstarterPath)
		quickstarterName := filepath.Base(quickstarterPath)

		// Run each quickstarter test in a subtest to avoid exiting early
		// when t.Fatal is used.
		t.Run(quickstarterName, func(t *testing.T) {
			s, err := readSteps(testdataPath)
			if err != nil {
				t.Fatal(err)
			}

			for i, step := range s.Steps {
				// Step might overwrite component ID
				if len(step.ComponentID) == 0 {
					step.ComponentID = s.ComponentID
				}
				fmt.Printf("Run step #%d (%s) of quickstarter %s ...\n", (i + 1), step.Type, quickstarterName)

				repoName := fmt.Sprintf("%s-%s", strings.ToLower(utils.PROJECT_NAME), step.ComponentID)

				if step.Type == "upload" {
					if len(step.UploadParams.Filename) == 0 {
						step.UploadParams.Filename = filepath.Base(step.UploadParams.File)
					}
					stdout, stderr, err := utils.RunScriptFromBaseDir("tests/scripts/upload-file-to-bitbucket.sh", []string{
						fmt.Sprintf("--bitbucket=%s", config["BITBUCKET_URL"]),
						fmt.Sprintf("--user=%s", config["CD_USER_ID"]),
						fmt.Sprintf("--password=%s", cdUserPassword),
						fmt.Sprintf("--project=%s", utils.PROJECT_NAME),
						fmt.Sprintf("--repository=%s", repoName),
						fmt.Sprintf("--file=%s/%s", testdataPath, step.UploadParams.File),
						fmt.Sprintf("--filename=%s", step.UploadParams.Filename),
					}, []string{})

					if err != nil {
						t.Fatalf(
							"Execution of `upload-file-to-bitbucket.sh` failed: \nStdOut: %s\nStdErr: %s\nErr: %s\n",
							stdout,
							stderr,
							err)
					} else {
						fmt.Printf("Uploaded file %s to %s\n", step.UploadParams.File, config["BITBUCKET_URL"])
					}
					continue
				}

				var request utils.RequestBuild
				var pipelineName string
				var jenkinsfile string
				var verify *TestStepVerify
				if step.Type == "provision" {
					// cleanup and create bb resources for this test
					err = recreateBitbucketRepo(config, utils.PROJECT_NAME, repoName)
					if err != nil {
						t.Fatal(err)
					}
					err = deleteOpenShiftResources(utils.PROJECT_NAME, step.ComponentID, utils.PROJECT_NAME_DEV)
					if err != nil {
						t.Fatal(err)
					}
					request = utils.RequestBuild{
						Repository: "ods-quickstarters",
						Branch:     config["ODS_GIT_REF"],
						Project:    config["ODS_BITBUCKET_PROJECT"],
						Env: []utils.EnvPair{
							{
								Name:  "ODS_NAMESPACE",
								Value: config["ODS_NAMESPACE"],
							},
							{
								Name:  "ODS_GIT_REF",
								Value: config["ODS_GIT_REF"],
							},
							{
								Name:  "ODS_IMAGE_TAG",
								Value: config["ODS_IMAGE_TAG"],
							},
							{
								Name:  "PROJECT_ID",
								Value: utils.PROJECT_NAME,
							},
							{
								Name:  "COMPONENT_ID",
								Value: step.ComponentID,
							},
							{
								Name:  "GIT_URL_HTTP",
								Value: fmt.Sprintf("%s/%s/%s.git", config["REPO_BASE"], utils.PROJECT_NAME, repoName),
							},
						},
					}
					// If quickstarter is overwritten, use that value. Otherwise
					// we use the quickstarter under test.
					if len(step.ProvisionParams.Quickstarter) > 0 {
						jenkinsfile = fmt.Sprintf("%s/Jenkinsfile", step.ProvisionParams.Quickstarter)
					} else {
						jenkinsfile = fmt.Sprintf("%s/Jenkinsfile", quickstarterName)
					}
					pipelineName = step.ProvisionParams.Pipeline
					verify = step.ProvisionParams.Verify
				} else if step.Type == "build" {
					request = utils.RequestBuild{
						Repository: repoName,
						Branch:     "master",
						Project:    utils.PROJECT_NAME,
						Env:        []utils.EnvPair{},
					}
					jenkinsfile = "Jenkinsfile"
					pipelineName = step.BuildParams.Pipeline
					verify = step.BuildParams.Verify
				}
				buildName, err := utils.RunJenkinsPipeline(jenkinsfile, request, pipelineName)
				if err != nil {
					t.Fatal(err)
				}

				verifyPipelineRun(t, step, verify, testdataPath, repoName, buildName, config)
			}
		})
	}
}

// collectTestableQuickstarters collects all subdirs of "dir" that contain
// a "testdata" directory.
func collectTestableQuickstarters(t *testing.T, dir string) []string {
	testableQuickstarters := []string{}
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.IsDir() {
			candidateDir := fmt.Sprintf("%s/%s", dir, f.Name())
			candidateTestdataDir := fmt.Sprintf("%s/testdata", candidateDir)
			if _, err := os.Stat(candidateTestdataDir); !os.IsNotExist(err) {
				testableQuickstarters = append(testableQuickstarters, candidateDir)
			}
		}
	}
	return testableQuickstarters
}

// verifyPipelineRun checks that all expected values from the TestStepVerify
// definition are present.
func verifyPipelineRun(t *testing.T, step TestStep, verify *TestStepVerify, testdataPath string, repoName string, buildName string, config map[string]string) {
	if verify == nil {
		fmt.Println("Nothing to verify for", buildName)
		return
	}

	sanitizedOdsGitRef := strings.Replace(config["ODS_GIT_REF"], "/", "_", -1)
	sanitizedOdsGitRef = strings.Replace(sanitizedOdsGitRef, "-", "_", -1)
	buildParts := strings.Split(buildName, "-")
	tmplData := TemplateData{
		ProjectID:           utils.PROJECT_NAME,
		ComponentID:         step.ComponentID,
		OdsNamespace:        config["ODS_NAMESPACE"],
		OdsGitRef:           config["ODS_GIT_REF"],
		OdsImageTag:         config["ODS_IMAGE_TAG"],
		OdsBitbucketProject: config["ODS_BITBUCKET_PROJECT"],
		SanitizedOdsGitRef:  sanitizedOdsGitRef,
		BuildNumber:         buildParts[len(buildParts)-1],
	}

	if len(verify.JenkinsStages) > 0 {
		fmt.Printf("Verifying Jenkins stages of %s ...\n", buildName)
		stages, err := utils.RetrieveJenkinsBuildStagesForBuild(utils.PROJECT_NAME_CD, buildName)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("%s pipeline run for %s returned:\n%s", step.Type, step.ComponentID, stages)
		err = utils.VerifyJenkinsStages(
			fmt.Sprintf("%s/%s", testdataPath, verify.JenkinsStages),
			stages,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(verify.SonarScan) > 0 {
		fmt.Printf("Verifying Sonar scan of %s ...\n", buildName)
		sonarscan, err := retrieveSonarScan(repoName, config)
		if err != nil {
			t.Fatal(err)
		}
		err = verifySonarScan(
			step.ComponentID,
			fmt.Sprintf("%s/%s", testdataPath, verify.SonarScan),
			sonarscan,
			tmplData,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(verify.RunAttachments) > 0 {
		fmt.Printf("Verifying Jenkins run attachments of %s ...\n", buildName)
		artifactsToVerify := []string{}
		for _, a := range verify.RunAttachments {
			artifactsToVerify = append(
				artifactsToVerify,
				renderTemplate(t, a, tmplData),
			)
		}
		err := utils.VerifyJenkinsRunAttachments(utils.PROJECT_NAME_CD, buildName, artifactsToVerify)
		if err != nil {
			t.Fatal(err)
		}
	}

	if verify.TestResults > 0 {
		fmt.Printf("Verifying unit tests of %s ...\n", buildName)
		stdout, stderr, err := utils.RunScriptFromBaseDir("tests/scripts/verify-jenkins-unittest-results.sh", []string{
			buildName,
			utils.PROJECT_NAME_CD,
			fmt.Sprintf("%d", verify.TestResults), // number of tests expected
		}, []string{})

		if err != nil {
			t.Fatalf("Could not find unit tests for build:%s\nstdout: %s\nstderr:%s\nerr: %s\n",
				buildName, stdout, stderr, err)
		}
	}

	if verify.OpenShiftResources != nil {
		var ocNamespace string
		if len(verify.OpenShiftResources.Namespace) > 0 {
			ocNamespace = renderTemplate(t, verify.OpenShiftResources.Namespace, tmplData)
		} else {
			ocNamespace = utils.PROJECT_NAME_DEV
		}
		fmt.Printf("Verifying OpenShift resources of %s in %s ...\n", step.ComponentID, ocNamespace)
		imageTags := []utils.ImageTag{}
		for _, it := range verify.OpenShiftResources.ImageTags {
			imageTags = append(
				imageTags,
				utils.ImageTag{
					Name: renderTemplate(t, it.Name, tmplData),
					Tag:  renderTemplate(t, it.Tag, tmplData),
				},
			)
		}
		resources := utils.Resources{
			Namespace:         ocNamespace,
			ImageTags:         imageTags,
			BuildConfigs:      renderTemplates(t, verify.OpenShiftResources.BuildConfigs, tmplData),
			DeploymentConfigs: renderTemplates(t, verify.OpenShiftResources.DeploymentConfigs, tmplData),
			Services:          renderTemplates(t, verify.OpenShiftResources.Services, tmplData),
			ImageStreams:      renderTemplates(t, verify.OpenShiftResources.ImageStreams, tmplData),
		}
		utils.CheckResources(resources, t)
	}
}

func renderTemplates(t *testing.T, tpls []string, tmplData TemplateData) []string {
	rendered := []string{}
	for _, tpl := range tpls {
		rendered = append(rendered, renderTemplate(t, tpl, tmplData))
	}
	return rendered
}

func renderTemplate(t *testing.T, tpl string, tmplData TemplateData) string {
	var attachmentBuffer bytes.Buffer
	tmpl, err := template.New("attachment").Parse(tpl)
	if err != nil {
		t.Fatalf("Error parsing template: %s", err)
	}
	tmplErr := tmpl.Execute(&attachmentBuffer, tmplData)
	if tmplErr != nil {
		t.Fatalf("Error rendering template: %s", tmplErr)
	}
	return attachmentBuffer.String()
}