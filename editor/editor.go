package editor

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/gdamore/tcell"
	"github.com/linde12/kod/rpc"
)

type Params map[string]interface{}

type Editor struct {
	screen    tcell.Screen
	Views     map[string]*View
	curViewID string
	rpc       *rpc.Connection

	styleMap *StyleMap

	// ui events
	events chan tcell.Event
	// user events
	RedrawEvents chan struct{}
}

func (e *Editor) CurView() *View {
	return e.Views[e.curViewID]
}

func (e *Editor) initScreen() {
	var err error

	e.screen, err = tcell.NewScreen()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err = e.screen.Init(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	e.screen.Clear()
}

func (e *Editor) handleEvent(ev tcell.Event) {
	switch ev.(type) {
	case *tcell.EventKey:
		e.CurView().HandleEvent(ev)
	}
}

func NewEditor(rw io.ReadWriter) *Editor {
	e := &Editor{}

	tcell.SetEncodingFallback(tcell.EncodingFallbackASCII)

	// screen event channel
	e.events = make(chan tcell.Event, 50)
	e.RedrawEvents = make(chan struct{}, 50)

	e.styleMap = NewStyleMap()
	e.Views = make(map[string]*View)

	e.rpc = rpc.NewConnection(rw)

	// Set theme, this might be removed when xi-editor has a config file
	e.rpc.Notify(&rpc.Request{
		Method: "set_theme",
		// TODO: Ability to change this would be nice...
		// Try "InspiredGitHub" or "Solarized (dark)"
		Params: rpc.Object{"theme_name": "base16-eighties.dark"},
	})

	return e
}

func (e *Editor) CloseView(v *View) {
	delete(e.Views, v.ID)
}

func (e *Editor) handleRequests() {
	for {
		msg := <-e.rpc.Messages

		switch msg.Value.(type) {
		case *rpc.Update:
			update := msg.Value.(*rpc.Update)
			view := e.Views[update.ViewID]
			view.ApplyUpdate(msg.Value.(*rpc.Update))
		case *rpc.DefineStyle:
			e.styleMap.DefineStyle(msg.Value.(*rpc.DefineStyle))
			e.screen.SetStyle(defaultStyle)
		case *rpc.ThemeChanged:
			themeChanged := msg.Value.(*rpc.ThemeChanged)
			theme := themeChanged.Theme

			bg := tcell.NewRGBColor(theme.Bg.ToRGB())
			defaultStyle = defaultStyle.Background(bg)
			fg := tcell.NewRGBColor(theme.Fg.ToRGB())
			defaultStyle = defaultStyle.Foreground(fg)

			e.screen.SetStyle(defaultStyle)

			log.Printf("Theme:%v", theme)
		}

		// TODO: Better way to signal redraw?
		e.RedrawEvents <- struct{}{}
	}
}

func (e *Editor) Start() {
	e.initScreen()
	defer e.screen.Fini()

	quit := make(chan bool, 1)

	go func() {
		for {
			if e.screen != nil {
				// feed events into channel
				e.events <- e.screen.PollEvent()
			}
		}
	}()

	path := os.Args[1]
	view, _ := NewView(path, e)
	e.Views[view.ID] = view
	e.curViewID = view.ID

	go e.handleRequests()

	// editor loop
	for {
		if len(e.Views) != 0 {
			curView := e.CurView()
			e.screen.Clear()
			curView.Draw()
			e.screen.Show()
		} else {
			quit <- true
		}

		var event tcell.Event
		select {
		case event = <-e.events:
		case <-e.RedrawEvents:
		case <-quit:
			e.screen.Fini()
			log.Println("bye")
			os.Exit(0)
		}

		for event != nil {
			switch ev := event.(type) {
			case *tcell.EventKey:
				switch ev.Key() {
				case tcell.KeyF1:
					close(quit)
				}
			case *tcell.EventResize:
				e.screen.Sync()
			}

			e.handleEvent(event)

			// continue handling events
			select {
			case event = <-e.events:
			default:
				event = nil
			}
		}
	}
}
