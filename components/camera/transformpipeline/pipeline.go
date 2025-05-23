// Package transformpipeline defines image sources that apply transforms on images, and can be composed into
// an image transformation pipeline. The image sources are not original generators of image, but require an image source
// from a real camera or video in order to function.
package transformpipeline

import (
	"context"
	"fmt"
	"image"

	"github.com/pkg/errors"
	"go.opencensus.io/trace"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/gostream"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/pointcloud"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/rimage"
	"go.viam.com/rdk/rimage/transform"
	"go.viam.com/rdk/robot"
	camerautils "go.viam.com/rdk/robot/web/stream/camera"
	"go.viam.com/rdk/utils"
)

var model = resource.DefaultModelFamily.WithModel("transform")

// ErrVideoSourceCreation is returned when creating a video source from a camera fails.
var ErrVideoSourceCreation = errors.New("failed to create video source from camera")

func init() {
	resource.RegisterComponent(
		camera.API,
		model,
		resource.Registration[camera.Camera, *transformConfig]{
			DeprecatedRobotConstructor: func(
				ctx context.Context,
				r any,
				conf resource.Config,
				logger logging.Logger,
			) (camera.Camera, error) {
				actualR, err := utils.AssertType[robot.Robot](r)
				if err != nil {
					return nil, err
				}
				newConf, err := resource.NativeConfig[*transformConfig](conf)
				if err != nil {
					return nil, err
				}
				sourceName := newConf.Source
				source, err := camera.FromRobot(actualR, sourceName)
				if err != nil {
					return nil, fmt.Errorf("no source camera for transform pipeline (%s): %w", sourceName, err)
				}
				vs, err := videoSourceFromCamera(ctx, source)
				if err != nil {
					return nil, fmt.Errorf("failed to create video source from camera: %w", err)
				}
				src, err := newTransformPipeline(ctx, vs, conf.ResourceName().AsNamed(), newConf, actualR, logger)
				if err != nil {
					return nil, err
				}
				return src, nil
			},
		})
}

// transformConfig specifies a stream and list of transforms to apply on images/pointclouds coming from a source camera.
type transformConfig struct {
	CameraParameters     *transform.PinholeCameraIntrinsics `json:"intrinsic_parameters,omitempty"`
	DistortionParameters *transform.BrownConrady            `json:"distortion_parameters,omitempty"`
	Source               string                             `json:"source"`
	Pipeline             []Transformation                   `json:"pipeline"`
}

// Validate ensures all parts of the config are valid.
func (cfg *transformConfig) Validate(path string) ([]string, []string, error) {
	var deps []string
	if len(cfg.Source) == 0 {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "source")
	}

	if cfg.CameraParameters != nil {
		if cfg.CameraParameters.Height < 0 || cfg.CameraParameters.Width < 0 {
			return nil, nil, errors.Errorf(
				"got illegal negative dimensions for width_px and height_px (%d, %d) fields set in intrinsic_parameters for transform camera",
				cfg.CameraParameters.Width, cfg.CameraParameters.Height,
			)
		}
	}

	deps = append(deps, cfg.Source)
	return deps, nil, nil
}

type videoSource struct {
	camera.Camera
	vs gostream.VideoSource
}

func (sc *videoSource) Stream(ctx context.Context, errHandlers ...gostream.ErrorHandler) (gostream.VideoStream, error) {
	if sc.vs != nil {
		return sc.vs.Stream(ctx, errHandlers...)
	}
	return sc.Stream(ctx, errHandlers...)
}

// videoSourceFromCamera is a hack to allow us to use Stream to pipe frames through the pipeline
// and still implement a camera resource.
// We prefer this methodology over passing Image bytes because each transform desires a image.Image over
// a raw byte slice. To use Image would be to wastefully encode and decode the frame multiple times.
func videoSourceFromCamera(ctx context.Context, cam camera.Camera) (camera.VideoSource, error) {
	if streamCam, ok := cam.(camera.VideoSource); ok {
		return streamCam, nil
	}
	vs, err := camerautils.VideoSourceFromCamera(ctx, cam)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrVideoSourceCreation, err)
	}
	return &videoSource{
		Camera: cam,
		vs:     vs,
	}, nil
}

func newTransformPipeline(
	ctx context.Context,
	source camera.VideoSource,
	named resource.Named,
	cfg *transformConfig,
	r robot.Robot,
	logger logging.Logger,
) (camera.VideoSource, error) {
	if source == nil {
		return nil, errors.New("no source camera for transform pipeline")
	}
	if len(cfg.Pipeline) == 0 {
		return nil, errors.New("pipeline has no transforms in it")
	}
	// check if the source produces a depth image or color image
	img, err := camera.DecodeImageFromCamera(ctx, "", nil, source)

	var streamType camera.ImageType
	if err != nil {
		streamType = camera.UnspecifiedStream
	} else if _, ok := img.(*rimage.DepthMap); ok {
		streamType = camera.DepthStream
	} else if _, ok := img.(*image.Gray16); ok {
		streamType = camera.DepthStream
	} else {
		streamType = camera.ColorStream
	}
	// loop through the pipeline and create the image flow
	pipeline := make([]camera.VideoSource, 0, len(cfg.Pipeline))
	lastSource, err := videoSourceFromCamera(ctx, source)
	if err != nil {
		return nil, err
	}
	for _, tr := range cfg.Pipeline {
		src, newStreamType, err := buildTransform(ctx, r, lastSource, streamType, tr)
		if err != nil {
			return nil, err
		}
		streamSrc, err := videoSourceFromCamera(ctx, src)
		if err != nil {
			return nil, err
		}
		pipeline = append(pipeline, streamSrc)
		lastSource = streamSrc
		streamType = newStreamType
	}
	cameraModel := camera.NewPinholeModelWithBrownConradyDistortion(cfg.CameraParameters, cfg.DistortionParameters)
	return camera.NewVideoSourceFromReader(
		ctx,
		transformPipeline{named, pipeline, lastSource, cfg.CameraParameters, logger},
		&cameraModel,
		streamType,
	)
}

type transformPipeline struct {
	resource.Named
	pipeline            []camera.VideoSource
	src                 camera.Camera
	intrinsicParameters *transform.PinholeCameraIntrinsics
	logger              logging.Logger
}

func (tp transformPipeline) Read(ctx context.Context) (image.Image, func(), error) {
	ctx, span := trace.StartSpan(ctx, "camera::transformpipeline::Read")
	defer span.End()
	img, err := camera.DecodeImageFromCamera(ctx, "", nil, tp.src)
	if err != nil {
		return nil, func() {}, err
	}
	return img, func() {}, nil
}

func (tp transformPipeline) NextPointCloud(ctx context.Context) (pointcloud.PointCloud, error) {
	ctx, span := trace.StartSpan(ctx, "camera::transformpipeline::NextPointCloud")
	defer span.End()
	if lastElem, ok := tp.pipeline[len(tp.pipeline)-1].(camera.PointCloudSource); ok {
		pc, err := lastElem.NextPointCloud(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "function NextPointCloud not defined for last videosource in transform pipeline")
		}
		return pc, nil
	}
	return nil, errors.New("function NextPointCloud not defined for last videosource in transform pipeline")
}

func (tp transformPipeline) Close(ctx context.Context) error {
	return nil
}
