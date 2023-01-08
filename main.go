package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/pubsub"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/smartdevicemanagement/v1"
)

type DeviceEvent struct {
	EventId          string          `json:"eventId"`
	Timestamp        string          `json:"timestamp"`
	RelationUpdate   *RelationUpdate `json:"relationUpdate"`
	ResourceUpdate   *ResourceUpdate `json:"resourceUpdate"`
	ResourceGroup    []string        `json:"resourceGroup"`
	EventThreadId    *string         `json:"eventThreadId"`
	EventThreadState *string         `json:"eventThreadState"`
	UserId           string          `json:"userId"`
}

func (e *DeviceEvent) format() string {
	return fmt.Sprintf(strings.Join([]string{
		"DeviceEvent",
		"* UserId: %v",
		"* EventId: %v",
		"* Timestamp: %v",
	}, "\n\t"), e.UserId, e.EventId, e.Timestamp)
}

// Notify a device (object) is registered to/deleted from/updated in the room(subject)
// If Subject is empty, it means structure/room is created/deleted
type RelationUpdate struct {
	Type    string `json:"type"`    // CREATED, DELETED, UPDATED
	Subject string `json:"subject"` // empty or "enterprises/project-id/structures/structure-id", (room or structure)
	Object  string `json:"object"`  // "enterprises/project-id/devices/device-id"
}

type ResourceUpdateEventType string

const (
	ResourceUpdateEventTypeDoorbellChime     = ResourceUpdateEventType("sdm.devices.events.DoorbellChime.Chime")
	ResourceUpdateEventTypeCameraMotion      = ResourceUpdateEventType("sdm.devices.events.CameraMotion.Motion")
	ResourceUpdateEventTypeCameraPerson      = ResourceUpdateEventType("sdm.devices.events.CameraPerson.Person")
	ResourceUpdateEventTypeCameraClipPreview = ResourceUpdateEventType("sdm.devices.events.CameraClipPreview.ClipPreview")
)

type ResourceUpdateEventDoorbellChime struct {
	EventSessionId string `json:"eventSessionId"`
	EventId        string `json:"eventId"`
}

func (p *ResourceUpdateEventDoorbellChime) format() string {
	if p == nil {
		return "DoorbellChimeEvent(nil)"
	}
	return fmt.Sprintf("DoorbellChimeEvent(EventSessionId: %v, EventId: %v)", p.EventSessionId, p.EventId)
}

type ResourceUpdateEventCameraMotion struct {
	EventSessionId string `json:"eventSessionId"`
	EventId        string `json:"eventId"`
}

func (p *ResourceUpdateEventCameraMotion) format() string {
	if p == nil {
		return "CameraMotionEvent(nil)"
	}
	return fmt.Sprintf("CameraMotionEvent(EventSessionId: %v, EventId: %v)", p.EventSessionId, p.EventId)
}

type ResourceUpdateEventCameraPerson struct {
	EventSessionId string `json:"eventSessionId"`
	EventId        string `json:"eventId"`
}

func (p *ResourceUpdateEventCameraPerson) format() string {
	if p == nil {
		return "CameraPersonEvent(nil)"
	}
	return fmt.Sprintf("CameraPersonEvent(EventSessionId: %v, EventId: %v)", p.EventSessionId, p.EventId)
}

type ResourceUpdateEventCameraClipPreview struct {
	EventSessionId string `json:"eventSessionId"`
	PreviewUrl     string `json:"previewUrl"`
}

func (p *ResourceUpdateEventCameraClipPreview) format() string {
	if p == nil {
		return "CameraClipPreview(nil)"
	}
	return fmt.Sprintf("CameraClipPreview(EventSessionId: %v, PreviewUrl: %v)", p.EventSessionId, p.PreviewUrl)
}

type ResourceUpdate struct {
	Name   string                                      `json:"name"` // "enterprises/project-id/devices/device-id",
	Traits map[string]json.RawMessage                  `json:"traits"`
	Events map[ResourceUpdateEventType]json.RawMessage `json:"events"`
}

// https://developers.google.com/nest/device-access/traits/device/camera-event-image#generateimage-request-fields
type GenerateImageRequestParam struct {
	EventId string `json:"eventId"`
}

// https://developers.google.com/nest/device-access/traits/device/camera-event-image#generateimage-response-fields
type GenerateImageResponse struct {
	Url   string `json:"url"`
	Token string `json:"token"`
}

// https://developers.google.com/nest/device-access/traits/device/camera-live-stream#generatewebrtcstream
type GenerateWebRtcStreamRequestParam struct {
	OfferSdp string `json:"offerSdp"`
}

// https://developers.google.com/nest/device-access/traits/device/camera-live-stream#extendwebrtcstream
type ExtendWebRtcStreamRequestParam struct {
	MediaSessionId string `json:"mediaSessionId"`
}

// https://developers.google.com/nest/device-access/traits/device/camera-live-stream#stopwebrtcstream
type StopWebRtcStreamRequestParam struct {
	MediaSessionId string `json:"mediaSessionId"`
}

type NestDoorbellEventProcessor struct {
	doorbellDeviceName   string
	client               *http.Client
	deviceAccessService  *smartdevicemanagement.Service
	outputDir            string
	outputFileNameFormat string
}

func (p *NestDoorbellEventProcessor) Init() error {
	if _, err := os.Stat(p.outputDir); os.IsNotExist(err) {
		if err := os.MkdirAll(p.outputDir, 0777); err != nil {
			return err
		}
	}
	return nil
}

func (p *NestDoorbellEventProcessor) Process(event *DeviceEvent) error {
	if event.ResourceUpdate != nil {
		return p.processResourceUpdateEvent(event)
	} else if event.RelationUpdate != nil {
		return p.processRelationUpdateEvent(event)
	}
	return errors.New("Unsupported event: " + event.format())
}

func (p *NestDoorbellEventProcessor) processResourceUpdateEvent(event *DeviceEvent) error {
	resourceUpdate := event.ResourceUpdate
	if raw, ok := resourceUpdate.Events[ResourceUpdateEventTypeDoorbellChime]; ok {
		var chimeEvent ResourceUpdateEventDoorbellChime
		var clipPreviewEvent *ResourceUpdateEventCameraClipPreview
		if err := json.Unmarshal(raw, &chimeEvent); err != nil {
			return err
		}
		if raw, ok := resourceUpdate.Events[ResourceUpdateEventTypeCameraClipPreview]; ok {
			clipPreviewEvent = &ResourceUpdateEventCameraClipPreview{}
			if err := json.Unmarshal(raw, clipPreviewEvent); err != nil {
				clipPreviewEvent = nil
			}
		}
		return p.processChimeEvent(&chimeEvent, clipPreviewEvent)
	} else if raw, ok := resourceUpdate.Events[ResourceUpdateEventTypeCameraMotion]; ok {
		var motionEvent ResourceUpdateEventCameraMotion
		var clipPreviewEvent *ResourceUpdateEventCameraClipPreview
		if err := json.Unmarshal(raw, &motionEvent); err != nil {
			return err
		}
		if raw, ok := resourceUpdate.Events[ResourceUpdateEventTypeCameraClipPreview]; ok {
			clipPreviewEvent = &ResourceUpdateEventCameraClipPreview{}
			if err := json.Unmarshal(raw, clipPreviewEvent); err != nil {
				clipPreviewEvent = nil
			}
		}
		return p.processMotionEvent(&motionEvent, clipPreviewEvent)
	} else if raw, ok := resourceUpdate.Events[ResourceUpdateEventTypeCameraPerson]; ok {
		var personEvent ResourceUpdateEventCameraPerson
		var clipPreviewEvent *ResourceUpdateEventCameraClipPreview
		if err := json.Unmarshal(raw, &personEvent); err != nil {
			return err
		}
		if raw, ok := resourceUpdate.Events[ResourceUpdateEventTypeCameraClipPreview]; ok {
			clipPreviewEvent = &ResourceUpdateEventCameraClipPreview{}
			if err := json.Unmarshal(raw, clipPreviewEvent); err != nil {
				clipPreviewEvent = nil
			}
		}
		return p.processPersonEvent(&personEvent, clipPreviewEvent)
	}
	var events = []string{}
	for key := range resourceUpdate.Events {
		events = append(events, string(key))
	}
	var traits = []string{}
	for key := range resourceUpdate.Traits {
		traits = append(traits, string(key))
	}
	return fmt.Errorf("unsupported resource update event:\n\t* user id(%v)\n\t* events(%v)\n\t* traits(%v)", event.UserId, strings.Join(events, ","), strings.Join(traits, ","))
}

func (p *NestDoorbellEventProcessor) processChimeEvent(chime *ResourceUpdateEventDoorbellChime, clipPreview *ResourceUpdateEventCameraClipPreview) error {
	log.Printf("processChimeEvent is not implemented yet: %v, %v", chime.format(), clipPreview.format())

	if clipPreview != nil {
		if err := p.downloadAndSaveCameraClipPreview(clipPreview); err != nil {
			return err
		}
	}
	return nil
}

func (p *NestDoorbellEventProcessor) processMotionEvent(motion *ResourceUpdateEventCameraMotion, clipPreview *ResourceUpdateEventCameraClipPreview) error {
	log.Printf("processMotionEvent is not implemented yet: %v, %v", motion.format(), clipPreview.format())
	if clipPreview != nil {
		if err := p.downloadAndSaveCameraClipPreview(clipPreview); err != nil {
			return err
		}
	}
	return nil
}

func (p *NestDoorbellEventProcessor) processPersonEvent(person *ResourceUpdateEventCameraPerson, clipPreview *ResourceUpdateEventCameraClipPreview) error {
	log.Printf("processPersonEvent is not implemented yet: %v, %v", person.format(), clipPreview.format())
	if clipPreview != nil {
		if err := p.downloadAndSaveCameraClipPreview(clipPreview); err != nil {
			return err
		}
	}
	return nil
}

func (p *NestDoorbellEventProcessor) processRelationUpdateEvent(event *DeviceEvent) error {
	log.Printf("processRelationUpdateEvent is not implemented yet: %v", event)
	return nil
}

func (p *NestDoorbellEventProcessor) downloadAndSaveCameraClipPreview(clipPreview *ResourceUpdateEventCameraClipPreview) error {
	resp, err := p.client.Get(clipPreview.PreviewUrl)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	extensions, err := mime.ExtensionsByType(resp.Header.Get("Content-Type"))
	if err != nil || len(extensions) == 0 {
		fmt.Printf("Failed to get extension type from content type(%v): err(%v)", resp.Header.Get("Content-Type"), err)
		extensions = []string{".video.unknown"}
	}
	i := 0
	fileNameFormat := time.Now().Format(p.outputFileNameFormat)
	fileName := ""
	for {
		fileName = filepath.Join(p.outputDir, strings.ReplaceAll(fileNameFormat, "{eventSessionId}", clipPreview.EventSessionId+"_"+strconv.Itoa(i))+extensions[0])
		if _, err := os.Stat(fileName); os.IsNotExist(err) {
			break
		}
		i = i + 1
		fmt.Printf("%v - %v\n", i, fileName)
	}
	outputDir := filepath.Dir(fileName)
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		if err := os.MkdirAll(outputDir, 0777); err != nil {
			return err
		}
	}
	file, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer file.Close()
	numWritten, err := io.Copy(file, resp.Body)
	if err != nil {
		return err
	}
	fmt.Printf("Wrote clipPreview for eventSession %v as %v (bytes: %v)\n", clipPreview.EventSessionId, extensions[0], numWritten)
	return nil
}

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config, tokFile string) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web %v", err)
	}
	return tok
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

func main() {
	var (
		projectId            = flag.String("nest-project-id", os.Getenv("NEST_PROJECT_ID"), "Device access console project id taken from https://console.nest.google.com/device-access e.g. enterprises/<project_id>")
		smartDeviceCredPath  = flag.String("smart-device-cred-path", "credentials.json", "path to google cloud oauth credential json file for smart device API")
		pubsubProject        = flag.String("pubsub-project-id", os.Getenv("PUBSUB_PROJECT_ID"), "google could project id for pubsub")
		pubsubCredPath       = flag.String("pubsub-cred-path", os.Getenv("PUBSUB_CRED_PATH"), "path to google cloud credential json file for pubsub")
		pubsubSubscriptionId = flag.String("pubsub-subscription-id", "test-subscription", "pubsub subscription id")
		outputDir            = flag.String("output-dir", "output", "output directory")
		outputFileNameFormat = flag.String("output-file-path-format", "2006/01/02/15/{eventSessionId}", "output file path format. Supports creating sub directory. go's time layout and {eventSessionId} is supported as variable.")
		//
		tokenPath = flag.String("token-path", "token.json", "file path to save access token/update token taken from smart device API oauth")
	)
	flag.Parse()

	ctx := context.Background()
	b, err := os.ReadFile(*smartDeviceCredPath)
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}
	config, err := google.ConfigFromJSON(b, smartdevicemanagement.SdmServiceScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(config, *tokenPath)

	svc, err := smartdevicemanagement.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatal(err)
	}
	r, err := svc.Enterprises.Devices.List(*projectId).Do()
	if err != nil {
		log.Fatal(err)
	}
	var doorbellDeviceName *string
	for _, i := range r.Devices {
		fmt.Printf("%v\n", i.Name)
		fmt.Printf("%v\n", i.Type)
		if i.Type == "sdm.devices.types.DOORBELL" {
			doorbellDeviceName = &i.Name
		}
	}
	if doorbellDeviceName == nil {
		log.Fatalln("Doorbell device not found in the account")
	} else {
		log.Println("Found", doorbellDeviceName)
	}

	pubsubClient, err := pubsub.NewClient(context.Background(), *pubsubProject, option.WithCredentialsFile(*pubsubCredPath))
	if err != nil {
		log.Fatal(err)
	}
	sub := pubsubClient.Subscription(*pubsubSubscriptionId)
	processor := NestDoorbellEventProcessor{
		doorbellDeviceName:   *doorbellDeviceName,
		client:               client,
		deviceAccessService:  svc,
		outputDir:            *outputDir,
		outputFileNameFormat: *outputFileNameFormat,
	}
	err = processor.Init()
	if err != nil {
		log.Fatal(err)
	}
	err = sub.Receive(context.Background(), func(ctx context.Context, m *pubsub.Message) {
		defer m.Ack()
		var event = DeviceEvent{}
		if err := json.Unmarshal(m.Data, &event); err != nil {
			log.Printf("Failed to unmarshal message: %v\n\t%v", err, m.Data)
			return
		}
		if err := processor.Process(&event); err != nil {
			log.Printf("Failed to process message: %v\n\t%v", err, m.Data)
			return
		}
	})
	if err != nil {
		log.Fatal(err)
	}
	for {
		time.Sleep(time.Second)
	}
}
