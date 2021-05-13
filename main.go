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
	"log"
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
	"barista.run/base/click"
	"barista.run/base/watchers/netlink"
	"barista.run/colors"
	"barista.run/format"
	"barista.run/group/modal"
	"barista.run/modules/clock"
	"barista.run/modules/cputemp"
	"barista.run/modules/diskio"
	"barista.run/modules/diskspace"
	"barista.run/modules/media"
	"barista.run/modules/meminfo"
	"barista.run/modules/meta/split"
	"barista.run/modules/sysinfo"
	"barista.run/modules/volume"
	"barista.run/modules/volume/alsa"
	"barista.run/outputs"
	"barista.run/pango"
	"barista.run/pango/icons/fontawesome"
	"barista.run/pango/icons/material"
	"barista.run/pango/icons/mdi"
	"barista.run/pango/icons/typicons"
	"barista.run/timing"

	colorful "github.com/lucasb-eyer/go-colorful"
	"github.com/martinlindhe/unit"
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

func main() {
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

	localdate := clock.Local().
		Output(time.Second, func(now time.Time) bar.Output {
			return outputs.Pango(
				pango.Icon("material-today").Alpha(0.6),
				now.Format("Mon Jan 2, 3:04 PM"),
			).OnClick(click.RunLeft("gsimplecal"))
		})

	makeTzClock := func(lbl, tzName string) bar.Module {
		c, err := clock.ZoneByName(tzName)
		if err != nil {
			panic(err)
		}
		return c.Output(time.Minute, func(now time.Time) bar.Output {
			return outputs.Pango(pango.Text(lbl).Smaller(), spacer, now.Format("15:04"))
		})
	}

	vol := volume.New(alsa.DefaultMixer()).Output(func(v volume.Volume) bar.Output {
		if v.Mute {
			return outputs.
				Pango(pango.Icon("fa-volume-mute").Alpha(0.8), spacer, "MUT").
				Color(colors.Scheme("degraded"))
		}
		iconName := "off"
		pct := v.Pct()
		if pct > 66 {
			iconName = "up"
		} else if pct > 33 {
			iconName = "down"
		}
		return outputs.Pango(
			pango.Icon("fa-volume-"+iconName).Alpha(0.6),
			spacer,
			pango.Textf("%2d%%", pct),
		)
	})

	loadAvg := sysinfo.New().Output(func(s sysinfo.Info) bar.Output {
		out := outputs.Pango(
			pango.Icon("mdi-desktop-tower").Alpha(0.6),
			pango.Textf("%0.2f", s.Loads[0]),
		)
		// Load averages are unusually high for a few minutes after boot.
		if s.Uptime < 10*time.Minute {
			// so don't add colours until 10 minutes after system start.
			return out
		}
		threshold(out,
			s.Loads[0] > 128 || s.Loads[2] > 64,
			s.Loads[0] > 64 || s.Loads[2] > 32,
			s.Loads[0] > 32 || s.Loads[2] > 16,
		)
		out.OnClick(click.Left(func() {
			mainModalController.Toggle("sysinfo")
		}))
		return out
	})

	loadAvgDetail := sysinfo.New().Output(func(s sysinfo.Info) bar.Output {
		return pango.Textf("%0.2f %0.2f", s.Loads[1], s.Loads[2]).Smaller()
	})

	uptime := sysinfo.New().Output(func(s sysinfo.Info) bar.Output {
		u := s.Uptime
		var uptimeOut *pango.Node
		if u.Hours() < 24 {
			uptimeOut = pango.Textf("%d:%02d",
				int(u.Hours()), int(u.Minutes())%60)
		} else {
			uptimeOut = pango.Textf("%dd%02dh",
				int(u.Hours()/24), int(u.Hours())%24)
		}
		return pango.Icon("mdi-trending-up").Alpha(0.6).Concat(uptimeOut)
	})

	freeMem := meminfo.New().Output(func(m meminfo.Info) bar.Output {
		out := outputs.Pango(
			pango.Icon("material-memory").Alpha(0.8),
			format.IBytesize(m.Available()),
		)
		freeGigs := m.Available().Gigabytes()
		threshold(out,
			freeGigs < 1,
			freeGigs < 2,
			freeGigs < 3,
			freeGigs > 12)
		out.OnClick(click.Left(func() {
			mainModalController.Toggle("sysinfo")
		}))
		return out
	})

	swapMem := meminfo.New().Output(func(m meminfo.Info) bar.Output {
		return outputs.Pango(
			pango.Icon("mdi-swap-horizontal").Alpha(0.8),
			format.IBytesize(m["SwapTotal"]-m["SwapFree"]), spacer,
			pango.Textf("(% 2.0f%%)", (1-m.FreeFrac("Swap"))*100.0).Small(),
		)
	})

	temp := cputemp.New().
		RefreshInterval(2 * time.Second).
		Output(func(temp unit.Temperature) bar.Output {
			out := outputs.Pango(
				pango.Icon("mdi-fan").Alpha(0.6), spacer,
				pango.Textf("%2d℃", int(temp.Celsius())),
			)
			threshold(out,
				temp.Celsius() > 90,
				temp.Celsius() > 70,
				temp.Celsius() > 60,
			)
			return out
		})

	sub := netlink.Any()
	sub.Unsubscribe()
	formatDiskSpace := func(i diskspace.Info, icon string) bar.Output {
		out := outputs.Pango(
			pango.Icon(icon).Alpha(0.7), spacer, format.IBytesize(i.Available))
		return threshold(out,
			i.Available.Gigabytes() < 1,
			i.AvailFrac() < 0.05,
			i.AvailFrac() < 0.1,
		)
	}

	rootDev := deviceForMountPath("/")
	var homeDiskspace bar.Module
	if deviceForMountPath(home()) != rootDev {
		homeDiskspace = diskspace.New(home()).Output(func(i diskspace.Info) bar.Output {
			return formatDiskSpace(i, "typecn-home-outline")
		})
	}
	rootDiskspace := diskspace.New("/").Output(func(i diskspace.Info) bar.Output {
		return formatDiskSpace(i, "fa-hdd")
	})

	mainDiskio := diskio.New(strings.TrimPrefix(rootDev, "/dev/")).
		Output(func(r diskio.IO) bar.Output {
			return pango.Icon("mdi-swap-vertical").
				Concat(spacer).
				ConcatText(format.IByterate(r.Total()))
		})

	mediaSummary, mediaDetail := split.New(media.Auto().Output(mediaFormatFunc), 1)

	activeTaskModule := &PollingModule{
		"5s",
		func(s bar.Sink) {
			activeTaskSegment := outputs.Pango(
				pango.Text("[act]").Alpha(0.6).ExtraCondensed(),
				spacer,
				pango.Textf(activetask.GetTaskMessage()),
			)
			activeTaskSegment.Align(bar.AlignEnd)
			s.Output(activeTaskSegment)
		},
	}

	upcomingClient := upcoming.DefaultClient()
	upcomingModule := &PollingModule{
		"2s",
		func(s bar.Sink) {
			list, err := upcomingClient.List(upcoming.ListOpts{})
			if err != nil {
				log.Printf("Error getting upcoming list: %s", err)
			}
			message := "No upcoming"
			if len(list) > 0 {
				message = list[len(list)-1].HumanizeDuration()
			}
			activeTaskSegment := outputs.Pango(
				pango.Text("[up]").Alpha(0.6).ExtraCondensed(),
				spacer,
				pango.Textf(message),
			)

			activeTaskSegment.Align(bar.AlignEnd)
			s.Output(activeTaskSegment)
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
			))
		},
	}

	mainModal := modal.New()
	sysMode := mainModal.Mode("sysinfo").
		SetOutput(makeIconOutput("mdi-chart-areaspline")).
		Add(loadAvg).
		Detail(loadAvgDetail, uptime).
		Add(freeMem).
		Detail(swapMem, temp)
	if homeDiskspace != nil {
		sysMode.Detail(homeDiskspace)
	}
	sysMode.Detail(rootDiskspace, mainDiskio)
	mainModal.Mode("media").
		SetOutput(makeIconOutput("mdi-music-box")).
		Add(vol, mediaSummary).
		Detail(mediaDetail)
	mainModal.Mode("timezones").
		SetOutput(makeIconOutput("material-access-time")).
		Detail(makeTzClock("Seattle", "America/Los_Angeles")).
		Detail(makeTzClock("New York", "America/New_York")).
		Detail(makeTzClock("UTC", "Etc/UTC")).
		Detail(makeTzClock("Seoul", "Asia/Seoul")).
		Add(localdate)

	var mm bar.Module
	mm, mainModalController = mainModal.Build()
	panic(barista.Run(activeTaskModule, upcomingModule, todayRemainingModule, mm))
}
