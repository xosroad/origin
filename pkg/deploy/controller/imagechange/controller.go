package imagechange

import (
	"fmt"

	"github.com/golang/glog"

	utilruntime "k8s.io/kubernetes/pkg/util/runtime"
	"k8s.io/kubernetes/pkg/util/sets"

	"github.com/openshift/origin/pkg/client"
	deployapi "github.com/openshift/origin/pkg/deploy/api"
	deployutil "github.com/openshift/origin/pkg/deploy/util"
	imageapi "github.com/openshift/origin/pkg/image/api"
)

// ImageChangeController increments the version of a deployment config which has an image
// change trigger when a tag update to a triggered ImageStream is detected.
//
// Use the ImageChangeControllerFactory to create this controller.
type ImageChangeController struct {
	listDeploymentConfigs func() ([]*deployapi.DeploymentConfig, error)
	client                client.Interface
}

// fatalError is an error which can't be retried.
type fatalError string

func (e fatalError) Error() string {
	return fmt.Sprintf("fatal error handling image stream: %s", string(e))
}

// Handle processes image change triggers associated with imagestream.
func (c *ImageChangeController) Handle(stream *imageapi.ImageStream) error {
	configs, err := c.listDeploymentConfigs()
	if err != nil {
		return fmt.Errorf("couldn't get list of deployment configs while handling image stream %q: %v", imageapi.LabelForStream(stream), err)
	}

	// Find any configs which should be updated based on the new image state
	configsToUpdate := []*deployapi.DeploymentConfig{}
	for _, config := range configs {
		glog.V(4).Infof("Detecting image changes for deployment config %q", deployutil.LabelForDeploymentConfig(config))
		hasImageChange := false

		for j := range config.Spec.Triggers {
			// because config can be copied during this loop, make sure we load from config for subsequent loops
			trigger := config.Spec.Triggers[j]
			params := trigger.ImageChangeParams

			// Only automatic image change triggers should fire
			if trigger.Type != deployapi.DeploymentTriggerOnImageChange {
				continue
			}

			// All initial deployments should have their images resolved in order to
			// be able to work and not try to pull non-existent images from DockerHub.
			// Deployments with automatic set to false that have been deployed at least
			// once shouldn't have their images updated.
			if (!params.Automatic || config.Spec.Paused) && len(params.LastTriggeredImage) > 0 {
				continue
			}

			// Check if the image stream matches the trigger
			if !triggerMatchesImage(config, params, stream) {
				continue
			}

			_, tag, ok := imageapi.SplitImageStreamTag(params.From.Name)
			if !ok {
				glog.Warningf("Invalid image stream tag %q in %q", params.From.Name, deployutil.LabelForDeploymentConfig(config))
				continue
			}

			// Find the latest tag event for the trigger tag
			latestEvent := imageapi.LatestTaggedImage(stream, tag)
			if latestEvent == nil {
				glog.V(5).Infof("Couldn't find latest tag event for tag %q in image stream %q", tag, imageapi.LabelForStream(stream))
				continue
			}

			// Ensure a change occurred
			if len(latestEvent.DockerImageReference) == 0 || latestEvent.DockerImageReference == params.LastTriggeredImage {
				glog.V(4).Infof("No image changes for deployment config %q were detected", deployutil.LabelForDeploymentConfig(config))
				continue
			}

			names := sets.NewString(params.ContainerNames...)
			for i := range config.Spec.Template.Spec.Containers {
				container := &config.Spec.Template.Spec.Containers[i]
				if !names.Has(container.Name) {
					continue
				}

				if !hasImageChange {
					// create a copy prior to mutation
					result, err := deployutil.DeploymentConfigDeepCopy(config)
					if err != nil {
						utilruntime.HandleError(err)
						continue
					}
					config = result
					container = &config.Spec.Template.Spec.Containers[i]
					params = config.Spec.Triggers[j].ImageChangeParams
				}

				// Update the image
				container.Image = latestEvent.DockerImageReference
				// Log the last triggered image ID
				params.LastTriggeredImage = latestEvent.DockerImageReference
				hasImageChange = true
			}
		}

		if hasImageChange {
			configsToUpdate = append(configsToUpdate, config)
		}
	}

	// Attempt to regenerate all configs which may contain image updates
	anyFailed := false
	for _, config := range configsToUpdate {
		if _, err := c.client.DeploymentConfigs(config.Namespace).Update(config); err != nil {
			anyFailed = true
			glog.V(2).Infof("Couldn't update deployment config %q: %v", deployutil.LabelForDeploymentConfig(config), err)
		}
	}

	if anyFailed {
		return fmt.Errorf("couldn't update some deployment configs for trigger on image stream %q", imageapi.LabelForStream(stream))
	}

	glog.V(5).Infof("Updated %d deployment configs for trigger on image stream %q", len(configsToUpdate), imageapi.LabelForStream(stream))
	return nil
}

// triggerMatchesImages decides whether a given trigger for config matches the provided image stream.
func triggerMatchesImage(config *deployapi.DeploymentConfig, params *deployapi.DeploymentTriggerImageChangeParams, stream *imageapi.ImageStream) bool {
	namespace := params.From.Namespace
	if len(namespace) == 0 {
		namespace = config.Namespace
	}
	name, _, ok := imageapi.SplitImageStreamTag(params.From.Name)
	return stream.Namespace == namespace && stream.Name == name && ok
}
