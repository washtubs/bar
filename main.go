// Copyright 2017 Google Inc.
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

// sample-bar demonstrates a sample i3bar built using barista.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/washtubs/activetask"
	"github.com/washtubs/upcoming"

	"barista.run"
	"barista.run/bar"
	"barista.run/colors"
	"barista.run/group/modal"
	"barista.run/modules/media"
	"barista.run/outputs"
	"barista.run/pango"
	"barista.run/pango/icons/fontawesome"
	"barista.run/pango/icons/material"
	"barista.run/pango/icons/mdi"
	"barista.run/pango/icons/typicons"
	"barista.run/timing"

	colorful "github.com/lucasb-eyer/go-colorful"
)

//func main() {
//barista.Run(
//clock.Local(),
//meminfo.New(),
//netinfo.New(),
//sysinfo.New(),
//wlan.Any(),
//battery.All(),
//)
//}

var spacer = pango.Text(" ").XXSmall()
var mainModalController modal.Controller

type PollingModule struct {
	interval string
	output   func(s bar.Sink)
}

// Stream starts the module.
func (m *PollingModule) Stream(s bar.Sink) {
	scheduler := timing.NewScheduler()
	duration, err := time.ParseDuration(m.interval)
	if err != nil {
		panic(err)
	}
	scheduler.Every(duration)
	for {
		m.output(s)
		<-scheduler.C
	}
}

func truncate(in string, l int) string {
	fromStart := false
	if l < 0 {
		fromStart = true
		l = -l
	}
	inLen := len([]rune(in))
	if inLen <= l {
		return in
	}
	if fromStart {
		return "⋯" + string([]rune(in)[inLen-l+1:])
	}
	return string([]rune(in)[:l-1]) + "⋯"
}

func hms(d time.Duration) (h int, m int, s int) {
	h = int(d.Hours())
	m = int(d.Minutes()) % 60
	s = int(d.Seconds()) % 60
	return
}

func formatMediaTime(d time.Duration) string {
	h, m, s := hms(d)
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func makeMediaIconAndPosition(m media.Info) *pango.Node {
	iconAndPosition := pango.Icon("fa-music").Color(colors.Hex("#f70"))
	if m.PlaybackStatus == media.Playing {
		iconAndPosition.Append(spacer,
			pango.Textf("%s/", formatMediaTime(m.Position())))
	}
	if m.PlaybackStatus == media.Paused || m.PlaybackStatus == media.Playing {
		iconAndPosition.Append(spacer,
			pango.Textf("%s", formatMediaTime(m.Length)))
	}
	return iconAndPosition
}

func mediaFormatFunc(m media.Info) bar.Output {
	if m.PlaybackStatus == media.Stopped || m.PlaybackStatus == media.Disconnected {
		return nil
	}
	artist := truncate(m.Artist, 35)
	title := truncate(m.Title, 70-len(artist))
	if len(title) < 35 {
		artist = truncate(m.Artist, 35-len(title))
	}
	var iconAndPosition bar.Output
	if m.PlaybackStatus == media.Playing {
		iconAndPosition = outputs.Repeat(func(time.Time) bar.Output {
			return makeMediaIconAndPosition(m)
		}).Every(time.Second)
	} else {
		iconAndPosition = makeMediaIconAndPosition(m)
	}
	return outputs.Group(iconAndPosition, outputs.Pango(title, " - ", artist))
}

func home(path ...string) string {
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	args := append([]string{usr.HomeDir}, path...)
	return filepath.Join(args...)
}

func deviceForMountPath(path string) string {
	mnt, _ := exec.Command("df", "-P", path).Output()
	lines := strings.Split(string(mnt), "\n")
	if len(lines) > 1 {
		devAlias := strings.Split(lines[1], " ")[0]
		dev, _ := exec.Command("realpath", devAlias).Output()
		devStr := strings.TrimSpace(string(dev))
		if devStr != "" {
			return devStr
		}
		return devAlias
	}
	return ""
}

type freegeoipResponse struct {
	Lat float64 `json:"latitude"`
	Lng float64 `json:"longitude"`
}

func whereami() (lat float64, lng float64, err error) {
	resp, err := http.Get("https://freegeoip.app/json/")
	if err != nil {
		return 0, 0, err
	}
	var res freegeoipResponse
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return 0, 0, err
	}
	return res.Lat, res.Lng, nil
}

func makeIconOutput(key string) *bar.Segment {
	return outputs.Pango(spacer, pango.Icon(key), spacer)
}

func threshold(out *bar.Segment, urgent bool, color ...bool) *bar.Segment {
	if urgent {
		return out.Urgent(true)
	}
	colorKeys := []string{"bad", "degraded", "good"}
	for i, c := range colorKeys {
		if len(color) > i && color[i] {
			return out.Color(colors.Scheme(c))
		}
	}
	return out
}

func run() {
	err := material.Load(home("Github/material-design-icons"))
	if err != nil {
		panic(err)
	}
	err = mdi.Load(home("Github/MaterialDesign-Webfont"))
	if err != nil {
		panic(err)
	}
	err = typicons.Load(home("Github/typicons.font"))
	if err != nil {
		panic(err)
	}
	err = fontawesome.Load(home("Github/Font-Awesome"))
	if err != nil {
		panic(err)
	}

	colors.LoadBarConfig()
	bg := colors.Scheme("background")
	fg := colors.Scheme("statusline")
	if fg != nil && bg != nil {
		_, _, v := fg.Colorful().Hsv()
		if v < 0.3 {
			v = 0.3
		}
		colors.Set("bad", colorful.Hcl(40, 1.0, v).Clamped())
		colors.Set("degraded", colorful.Hcl(90, 1.0, v).Clamped())
		colors.Set("good", colorful.Hcl(120, 1.0, v).Clamped())
	}

	maxWidth := 3700
	minWidths := make(map[string]int)
	minWidths["upcoming"] = 1400
	minWidths["remaining"] = 400
	total := 0
	for w := range minWidths {
		total += minWidths[w]
	}

	minWidths["activetask"] = maxWidth - total

	activeTaskModule := &PollingModule{
		"5s",
		func(s bar.Sink) {
			activeTaskSegment := outputs.Pango(
				pango.Text("[act]").Alpha(0.6).ExtraCondensed(),
				spacer,
				pango.Textf(activetask.GetTaskMessage()),
			).MinWidth(minWidths["activetask"])
			activeTaskSegment.Align(bar.AlignStart)
			//activeTaskSegment.Padding(100)
			group := outputs.Group(activeTaskSegment)
			s.Output(group)
		},
	}

	upcomingClient := upcoming.NewClient("")
	upcomingModule := &PollingModule{
		"2s",
		func(s bar.Sink) {
			list, err := upcomingClient.List(upcoming.ListOpts{})
			if err != nil {
				log.Printf("Error getting upcoming list: %s", err)
			}
			var upcomingSegment *bar.Segment
			if len(list) > 0 {
				next := list[len(list)-1]
				message := next.HumanizeDuration() + ": " + next.Title
				upcomingSegment = outputs.Pango(
					pango.Text("[up]").Alpha(0.6).ExtraCondensed(),
					spacer,
					pango.Textf(message),
				)
				if next.When.Before(time.Now().Add(time.Minute * 1)) {
					upcomingSegment = upcomingSegment.Color(colors.Scheme("bad"))
				} else if next.When.Before(time.Now().Add(time.Minute * 5)) {
					upcomingSegment = upcomingSegment.Color(colors.Scheme("degraded"))
				}
			} else {
				upcomingSegment = outputs.Pango(
					pango.Text("[up]").Alpha(0.6).ExtraCondensed(),
					spacer,
					pango.Textf("No upcoming"),
				)
			}
			upcomingSegment = upcomingSegment.MinWidth(minWidths["upcoming"])

			//upcomingSegment.Align(bar.AlignEnd)
			s.Output(upcomingSegment)
		},
	}

	todayRemainingModule := &PollingModule{
		"10m",
		func(s bar.Sink) {
			cmd := exec.Command("today-remaining-barista")
			buf, err := cmd.CombinedOutput()
			if err != nil {
				log.Printf("Failed to get today-remaining tasks: %s", err)
			}
			message := strings.TrimSpace(string(buf))
			if len(message) == 0 {
				s.Output(nil)
				return
			}

			s.Output(outputs.Pango(
				pango.Icon("typecn-home-outline").Alpha(0.6),
				spacer,
				pango.Textf(message),
			).MinWidth(minWidths["remaining"]))
		},
	}

	for {
		err := barista.Run(activeTaskModule, upcomingModule, todayRemainingModule)
		if err != nil {
			log.Printf("ERROR: %s", err.Error())
		}
	}
}
func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Fatalf("Panic: %v", r)
		}
	}()
	log.Printf("hi")
	run()
}
