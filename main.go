package main

import (
	"fmt"
	"os/user"
	"path/filepath"
	"time"

	"barista.run"
	"barista.run/bar"
	"barista.run/base/click"
	"barista.run/colors"
	"barista.run/format"
	"barista.run/modules/battery"
	"barista.run/modules/clock"
	"barista.run/modules/cputemp"
	"barista.run/modules/media"
	"barista.run/modules/meminfo"
	"barista.run/modules/netspeed"
	"barista.run/modules/sysinfo"
	"barista.run/outputs"
	"barista.run/pango"
	"barista.run/pango/icons/mdi"

	"github.com/martinlindhe/unit"
)

var spacer = pango.Text(" ").XXSmall()

func truncate(in string, l int) string {
	if len([]rune(in)) <= l {
		return in
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

func mediaFormatFunc(m media.Info) bar.Output {
	if m.PlaybackStatus == media.Stopped || m.PlaybackStatus == media.Disconnected {
		return nil
	}
	artist := truncate(m.Artist, 20)
	title := truncate(m.Title, 40-len(artist))
	if len(title) < 20 {
		artist = truncate(m.Artist, 40-len(title))
	}
	iconAndPosition := pango.Icon("fa-music").Color(colors.Hex("#f70"))
	if m.PlaybackStatus == media.Playing {
		iconAndPosition.Append(
			spacer, pango.Textf("%s/%s",
				formatMediaTime(m.Position()),
				formatMediaTime(m.Length)),
		)
	}
	return outputs.Pango(iconAndPosition, spacer, title, " - ", artist)
}

var startTaskManager = click.RunLeft("gnome-system-monitor")

func dataDir(path string) string {
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	return filepath.Join(usr.HomeDir, ".local/lib/barista", path)
}

func main() {
	mdi.Load(dataDir("MaterialDesign-Webfont"))

	colors.LoadFromMap(map[string]string{
		"good":     "#6d6",
		"degraded": "#dd6",
		"bad":      "#d66",
		"dim-icon": "#777",
	})

	localtime := clock.Local().
		Output(time.Second, func(now time.Time) bar.Output {
			return outputs.Pango(
				pango.Icon("mdi-clock").Color(colors.Scheme("dim-icon")),
				spacer,
				now.Format("Mon Jan 2 15:04:05"),
			).OnClick(click.RunLeft("gsimplecal"))
		})

	buildBattOutput := func(i battery.Info, disp *pango.Node) *bar.Segment {
		if i.Status == battery.Disconnected {
			return nil
		}
		iconName := "battery"
		if i.Status == battery.Charging {
			iconName += "-charging"
		}
		tenth := i.RemainingPct() / 10
		switch {
		case tenth == 0:
			iconName += "-outline"
		case tenth < 10:
			iconName += fmt.Sprintf("-%d0", tenth)
		}
		out := outputs.Pango(pango.Icon("mdi-"+iconName), spacer, disp)
		switch {
		case i.RemainingPct() <= 5:
			out.Urgent(true)
		case i.RemainingPct() <= 15:
			out.Color(colors.Scheme("bad"))
		case i.RemainingPct() <= 25:
			out.Color(colors.Scheme("degraded"))
		}
		return out
	}

	showBattPct := func(i battery.Info) bar.Output {
		out := buildBattOutput(i, pango.Textf("%d%%", i.RemainingPct()))
		if out == nil {
			return nil
		}
		return out
	}
	batt0 := battery.Named("BAT0").Output(showBattPct)
	batt1 := battery.Named("BAT1").Output(showBattPct)

	loadAvg := sysinfo.New().Output(func(s sysinfo.Info) bar.Output {
		load5Min := s.Loads[1]

		out := outputs.Pango(
			pango.Icon("mdi-heart-pulse").Color(colors.Scheme("dim-icon")),
			spacer,
			pango.Textf("%0.2f", load5Min),
		)

		// Load averages are unusually high for a few minutes after boot.
		if s.Uptime < 10*time.Minute {
			// so don't add colours until 10 minutes after system start.
			return out
		}
		switch {
		case load5Min > 8:
			out.Urgent(true)
		case load5Min > 4:
			out.Color(colors.Scheme("bad"))
		case load5Min > 1:
			out.Color(colors.Scheme("degraded"))
		}
		out.OnClick(startTaskManager)
		return out
	})

	freeMem := meminfo.New().Output(func(m meminfo.Info) bar.Output {
		out := outputs.Pango(
			pango.Icon("mdi-memory").Color(colors.Scheme("dim-icon")),
			format.IBytesize(m.Available()),
		)
		freeGigs := m.Available().Gigabytes()
		switch {
		case freeGigs < 0.5:
			out.Urgent(true)
		case freeGigs < 1:
			out.Color(colors.Scheme("bad"))
		case freeGigs < 2:
			out.Color(colors.Scheme("degraded"))
		case freeGigs > 12:
			out.Color(colors.Scheme("good"))
		}
		out.OnClick(startTaskManager)
		return out
	})

	temp := cputemp.New().
		RefreshInterval(2 * time.Second).
		Output(func(temp unit.Temperature) bar.Output {
			out := outputs.Pango(
				pango.Icon("mdi-fan").Color(colors.Scheme("dim-icon")),
				spacer,
				pango.Textf("%2d℃", int(temp.Celsius())),
			)
			switch {
			case temp.Celsius() > 90:
				out.Urgent(true)
			case temp.Celsius() > 70:
				out.Color(colors.Scheme("bad"))
			case temp.Celsius() > 60:
				out.Color(colors.Scheme("degraded"))
			}
			return out
		})

	net := netspeed.New("wlan0").
		RefreshInterval(time.Second).
		Output(func(s netspeed.Speeds) bar.Output {
			txColor := netColor(s.Tx, 1e5)
			rxColor := netColor(s.Rx, 1e5)
			return outputs.Pango(
				pango.Icon("mdi-upload-network").Color(txColor),
				spacer,
				pango.Icon("mdi-download-network").Color(rxColor),
			)
		})

	panic(barista.Run(
		media.New("spotify").Output(mediaFormatFunc),
		media.New("rhythmbox").Output(mediaFormatFunc),
		loadAvg,
		// XXX cpu
		temp,
		freeMem,
		net,
		batt0,
		batt1,
		localtime,
	))
}

func netColor(speed unit.Datarate, maxBytes int) colors.ColorfulColor {
	bytes := int(speed.BytesPerSecond())
	return grey(scale(bytes, maxBytes, 200) + 55)
}

func grey(n int) colors.ColorfulColor {
	if n < 0 {
		n = 0
	}
	if n > 255 {
		n = 255
	}
	h := fmt.Sprintf("%02x", n)
	return colors.Hex("#" + h + h + h)
}

func scale(v, vmax, intervals int) int {
	if v > vmax {
		v = vmax
	}
	if v < 0 {
		v = 0
	}
	return v * intervals / vmax
}

const vbarMinChar = 0x2580
const numVBars = 8

// XXX rework in terms of `scale` (and move it)
func vbar(v, max int) string {
	if v > max {
		v = max
	}
	if v < 0 {
		v = 0
	}
	i := v * numVBars / max
	if i == 0 {
		return ""
	}
	return string(rune(vbarMinChar + i))
}
