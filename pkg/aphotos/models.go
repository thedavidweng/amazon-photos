package aphotos

import (
	"encoding/json"
	"strings"
	"time"
)

type NodeKind string

const (
	NodeKindFile             NodeKind = "FILE"
	NodeKindFolder           NodeKind = "FOLDER"
	NodeKindVisualCollection NodeKind = "VISUAL_COLLECTION"
)

type NodeStatus string

const (
	NodeStatusAvailable NodeStatus = "AVAILABLE"
	NodeStatusTrash     NodeStatus = "TRASH"
)

type Node struct {
	ID                string              `json:"id"`
	Name              string              `json:"name"`
	Kind              NodeKind            `json:"kind"`
	Status            NodeStatus          `json:"status"`
	OwnerID           string              `json:"ownerId"`
	IsRoot            bool                `json:"isRoot"`
	Parents           []string            `json:"parents"`
	ParentMap         map[string][]string `json:"parentMap,omitempty"`
	CreatedDate       time.Time           `json:"createdDate"`
	ModifiedDate      time.Time           `json:"modifiedDate"`
	ContentProperties *ContentProperties  `json:"contentProperties,omitempty"`
	Settings          *NodeSettings       `json:"settings,omitempty"`
	Raw               json.RawMessage     `json:"-"`
}

type NodeSettings struct {
	Favorite bool `json:"favorite"`
	Hidden   bool `json:"hidden"`
}

type ContentProperties struct {
	Version     int              `json:"version,omitempty"`
	Size        int64            `json:"size,omitempty"`
	MD5         string           `json:"md5,omitempty"`
	ContentType string           `json:"contentType,omitempty"`
	Extension   string           `json:"extension,omitempty"`
	ContentDate time.Time        `json:"contentDate,omitempty"`
	Image       *ImageProperties `json:"image,omitempty"`
	Video       *VideoProperties `json:"video,omitempty"`
}

type ImageProperties struct {
	Width             int       `json:"width,omitempty"`
	Height            int       `json:"height,omitempty"`
	Make              string    `json:"make,omitempty"`
	Model             string    `json:"model,omitempty"`
	ISO               int       `json:"iso,omitempty"`
	ApertureValue     string    `json:"apertureValue,omitempty"`
	FocalLength       string    `json:"focalLength,omitempty"`
	ExposureTime      string    `json:"exposureTime,omitempty"`
	DateTime          time.Time `json:"dateTime,omitempty"`
	DateTimeOriginal  time.Time `json:"dateTimeOriginal,omitempty"`
	DateTimeDigitized time.Time `json:"dateTimeDigitized,omitempty"`
	Location          string    `json:"location,omitempty"`
}

type VideoProperties struct {
	Width      int     `json:"width,omitempty"`
	Height     int     `json:"height,omitempty"`
	Duration   float64 `json:"duration,omitempty"`
	VideoCodec string  `json:"videoCodec,omitempty"`
	AudioCodec string  `json:"audioCodec,omitempty"`
	FrameRate  float64 `json:"frameRate,omitempty"`
	Bitrate    int64   `json:"bitrate,omitempty"`
}

func (n *Node) UnmarshalJSON(data []byte) error {
	type rawNode struct {
		ID                string              `json:"id"`
		Name              string              `json:"name"`
		Kind              NodeKind            `json:"kind"`
		Status            NodeStatus          `json:"status"`
		OwnerID           string              `json:"ownerId"`
		IsRoot            bool                `json:"isRoot"`
		Parents           []string            `json:"parents"`
		ParentMap         map[string][]string `json:"parentMap,omitempty"`
		CreatedDate       string              `json:"createdDate"`
		ModifiedDate      string              `json:"modifiedDate"`
		ContentProperties *ContentProperties  `json:"contentProperties,omitempty"`
		Settings          *NodeSettings       `json:"settings,omitempty"`
		Info              *struct {
			NodeID string `json:"nodeId"`
		} `json:"info,omitempty"`
	}
	var aux rawNode
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	n.ID = aux.ID
	if n.ID == "" && aux.Info != nil {
		n.ID = aux.Info.NodeID
	}
	n.Name = aux.Name
	n.Kind = aux.Kind
	n.Status = aux.Status
	n.OwnerID = aux.OwnerID
	n.IsRoot = aux.IsRoot
	n.Parents = aux.Parents
	n.ParentMap = aux.ParentMap
	n.CreatedDate = parseOptionalUTCTime(aux.CreatedDate)
	n.ModifiedDate = parseOptionalUTCTime(aux.ModifiedDate)
	n.ContentProperties = aux.ContentProperties
	n.Settings = aux.Settings
	n.Raw = append(json.RawMessage(nil), data...)
	return nil
}

func (cp *ContentProperties) UnmarshalJSON(data []byte) error {
	type rawCP struct {
		Version     int              `json:"version,omitempty"`
		Size        flexInt64        `json:"size,omitempty"`
		MD5         string           `json:"md5,omitempty"`
		ContentType string           `json:"contentType,omitempty"`
		Extension   string           `json:"extension,omitempty"`
		ContentDate string           `json:"contentDate,omitempty"`
		Image       *ImageProperties `json:"image,omitempty"`
		Video       *VideoProperties `json:"video,omitempty"`
	}
	var aux rawCP
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	cp.Version = aux.Version
	cp.Size = int64(aux.Size)
	cp.MD5 = aux.MD5
	cp.ContentType = aux.ContentType
	cp.Extension = aux.Extension
	cp.ContentDate = parseOptionalUTCTime(aux.ContentDate)
	cp.Image = aux.Image
	cp.Video = aux.Video
	return nil
}

func (ip *ImageProperties) UnmarshalJSON(data []byte) error {
	type rawIP struct {
		Width             int    `json:"width,omitempty"`
		Height            int    `json:"height,omitempty"`
		Make              string `json:"make,omitempty"`
		Model             string `json:"model,omitempty"`
		ISO               int    `json:"iso,omitempty"`
		ApertureValue     string `json:"apertureValue,omitempty"`
		FocalLength       string `json:"focalLength,omitempty"`
		ExposureTime      string `json:"exposureTime,omitempty"`
		DateTime          string `json:"dateTime,omitempty"`
		DateTimeOriginal  string `json:"dateTimeOriginal,omitempty"`
		DateTimeDigitized string `json:"dateTimeDigitized,omitempty"`
		Location          string `json:"location,omitempty"`
	}
	var aux rawIP
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	ip.Width = aux.Width
	ip.Height = aux.Height
	ip.Make = aux.Make
	ip.Model = aux.Model
	ip.ISO = aux.ISO
	ip.ApertureValue = aux.ApertureValue
	ip.FocalLength = aux.FocalLength
	ip.ExposureTime = aux.ExposureTime
	ip.DateTime = parseOptionalUTCTime(aux.DateTime)
	ip.DateTimeOriginal = parseOptionalUTCTime(aux.DateTimeOriginal)
	ip.DateTimeDigitized = parseOptionalUTCTime(aux.DateTimeDigitized)
	ip.Location = aux.Location
	return nil
}

func parseOptionalUTCTime(raw string) time.Time {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z0700", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
