package clz

import (
	"io"

	"github.com/LagrangeDev/LagrangeGo/message"
)

type LocalVideo struct {
	File  string
	Thumb io.ReadSeeker
}

type LocalImage struct {
	Stream io.ReadSeeker
	File   string
	URL    string

	Flash    bool
	EffectID int32
}

func (e *LocalImage) Type() message.ElementType {
	return message.Image
}

func (e *LocalVideo) Type() message.ElementType {
	return message.Video
}