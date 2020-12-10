package bot

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/frizinak/hue/app"
	"github.com/frizinak/hue/hue"
)

const HueName = "hue-bot"

type HueBot struct {
	hue           *hue.App
	groupExcludes map[string]struct{}

	app  *app.App
	init bool
	err  error
}

func NewHueBot(ip, pass string, groupExcludes []string) *HueBot {
	filter := make(map[string]struct{}, len(groupExcludes))
	for _, f := range groupExcludes {
		filter[f] = struct{}{}
	}
	return &HueBot{hue: hue.New(ip, pass), groupExcludes: filter}
}

func (h *HueBot) Init() error {
	if h.init {
		return h.err
	}
	h.init = true
	h.err = h.hue.Init()
	if h.err == nil {
		h.app = app.New(h.hue)
	}
	return h.err
}

func (h *HueBot) Message(user string, args ...string) (string, string, error) {
	if err := h.Init(); err != nil {
		return HueName, "I am currently not available, contact an administrator", err
	}

	if len(args) == 0 || args[0] == "help" {
		cmds := []string{
			" - list",
			"       list all groups",
			" - list scenes",
			"       list all groups with scenes",
			" - <group> <setting>",
			"       <group>:   group name or id",
			"       <setting>: either a scene name or a hex color",
		}
		return HueName, strings.Join(cmds, "\n"), nil
	}

	switch args[0] {
	case "list":
		data := bytes.NewBuffer(nil)
		for _, g := range h.hue.Groups().Sort(hue.ID) {
			if _, ok := h.groupExcludes[g.Name]; ok {
				continue
			}

			fmt.Fprintf(data, "%-3s) %-30s\n", h.app.GID(g), g.Name)
			if len(args) > 1 && args[1] == "scenes" {
				for _, s := range g.Scenes().Sort(hue.Name) {
					fmt.Fprintf(data, "    %-30s\n", s.Name)
				}
			}
		}

		return HueName, data.String(), nil

	default:
		if len(args) < 2 {
			return HueName, "invalid command", nil
		}

		full := strings.ToLower(strings.Join(args, ""))

		var bestmatch *hue.Group
		matchL := 0
		groups := h.hue.Groups()
		for _, g := range groups {
			fullg := strings.ToLower(strings.ReplaceAll(g.Name, " ", ""))
			if strings.HasPrefix(full, fullg) && len(fullg) > matchL {
				matchL = len(fullg)
				bestmatch = g
			}
		}

		if bestmatch == nil {
			return HueName, "no such group", nil
		}

		rargs := make([]string, 0, len(args))
		for i := range args {
			matchL -= len(args[i])
			if matchL < 0 {
				rargs = append(rargs, args[i])
			}
		}

		if len(rargs) == 0 {
			return HueName, "missing color/scene", nil
		}

		c, err := h.app.HexColor(rargs[0])
		if err == nil {
			if err := h.hue.SetColor(bestmatch.Lights().Slice(), c); err != nil {
				return HueName, err.Error(), err
			}
			return HueName, "lights updated", nil
		}

		err = h.hue.SetGroupSceneByName(groups, strings.Join(rargs, " "))
		if err != nil {
			return HueName, err.Error(), nil
		}

		return HueName, "lights updated", nil
	}
}
