//Package remoton-client-desktop
//GUI for sharing desktop.
//
//Environment Vars:
//  * REMOTON_SERVER : set default remote server to connect

//+build linux,windows
package main

import (
	"crypto/tls"
	"flag"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/bit4bit/remoton"
	"github.com/bit4bit/remoton/common"

	log "github.com/Sirupsen/logrus"
	"github.com/mattn/go-gtk/gdk"
	"github.com/mattn/go-gtk/glib"
	"github.com/mattn/go-gtk/gtk"
)

var (
	clremoton       *clientRemoton
	machinePassword string
	insecure        = flag.Bool("insecure", false, "skip verify tls")
)

func main() {
	flag.Parse()

	runtime.GOMAXPROCS(runtime.NumCPU())
	common.SetDefaultGtkTheme()

	machinePassword = remoton.GenerateAuthUser()
	clremoton = newClient(&remoton.Client{Prefix: "/remoton", TLSConfig: &tls.Config{}})
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGINT, syscall.SIGABRT, syscall.SIGKILL, syscall.SIGTERM)
	go func() {
		<-sigs
		clremoton.Terminate()
	}()
	gtk.Init(nil)

	window := gtk.NewWindow(gtk.WINDOW_TOPLEVEL)
	window.SetPosition(gtk.WIN_POS_CENTER)
	window.SetTitle("REMOTON CLIENT")
	window.Connect("destroy", func(ctx *glib.CallbackContext) {
		gtk.MainQuit()
		clremoton.Terminate()
	}, "quit")
	window.SetIcon(common.GetIconGdkPixbuf())

	appLayout := gtk.NewVBox(false, 1)
	menu := gtk.NewMenuBar()
	appLayout.Add(menu)

	cascademenu := gtk.NewMenuItemWithMnemonic("_Help")
	menu.Append(cascademenu)
	submenu := gtk.NewMenu()
	cascademenu.SetSubmenu(submenu)

	menuitem := gtk.NewMenuItemWithMnemonic("_About")
	menuitem.Connect("activate", func() {
		dialog := common.GtkAboutDialog()
		dialog.SetProgramName("Client Desktop")
		dialog.SetComments("Share your desktop secure")
		dialog.Run()
		dialog.Destroy()
	})
	submenu.Append(menuitem)

	hpaned := gtk.NewHPaned()
	appLayout.Add(hpaned)
	statusbar := gtk.NewStatusbar()
	contextID := statusbar.GetContextId("remoton-desktop-client")

	//---
	//CONTROL
	//---
	frameControl := gtk.NewFrame("Controls")
	controlBox := gtk.NewVBox(false, 1)
	frameControl.Add(controlBox)

	controlBox.Add(gtk.NewLabel("MACHINE ID"))
	machineIDEntry := gtk.NewEntry()
	machineIDEntry.SetEditable(false)
	controlBox.Add(machineIDEntry)

	machineAuthEntry := gtk.NewEntry()
	machineAuthEntry.SetEditable(false)
	controlBox.Add(machineAuthEntry)

	controlBox.Add(gtk.NewLabel("Server"))
	serverEntry := gtk.NewEntry()
	serverEntry.SetText("127.0.0.1:9934")
	if os.Getenv("REMOTON_SERVER") != "" {
		serverEntry.SetText(os.Getenv("REMOTON_SERVER"))
		serverEntry.SetEditable(false)
	}
	controlBox.Add(serverEntry)

	controlBox.Add(gtk.NewLabel("Auth Server"))
	authServerEntry := gtk.NewEntry()
	authServerEntry.SetText("public")
	controlBox.Add(authServerEntry)

	var getCertFilename func() string

	localCert := filepath.Join(filepath.Dir(os.Args[0]), "cert.pem")
	if _, err := os.Stat(localCert); err == nil || os.IsExist(err) {
		controlBox.Add(gtk.NewLabel("Cert local"))
		getCertFilename = func() string {
			return localCert
		}
	} else if os.Getenv("REMOTON_CERT_FILE") != "" {
		controlBox.Add(gtk.NewLabel("Cert enviroment"))
		getCertFilename = func() string {
			return os.Getenv("REMOTON_CERT_FILE")
		}
	} else {
		btnCert := gtk.NewFileChooserButton("Cert", gtk.FILE_CHOOSER_ACTION_OPEN)
		getCertFilename = btnCert.GetFilename
		controlBox.Add(btnCert)
	}

	btnSrv := gtk.NewButtonWithLabel("Start")
	clremoton.VNC.OnConnection(func(addr net.Addr) {
		statusbar.Push(contextID, "Someone connected")
		log.Println("New connection from:" + addr.String())
	})
	btnSrv.Clicked(func() {
		if *insecure {
			clremoton.SetInsecure()
		} else {
			certPool, err := common.GetRootCAFromFile(getCertFilename())
			if err != nil {
				dialogError(window, err)
				return
			}
			clremoton.SetCertPool(certPool)
		}

		if !clremoton.Started() {
			log.Println("starting remoton")
			machinePassword = remoton.GenerateAuthUser()
			err := clremoton.Start(serverEntry.GetText(), authServerEntry.GetText(),
				machinePassword)

			if err != nil {
				dialogError(btnSrv.GetTopLevelAsWindow(), err)
				statusbar.Push(contextID, "Failed")
			} else {
				btnSrv.SetLabel("Stop")

				machineIDEntry.SetText(clremoton.MachineID())
				machineAuthEntry.SetText(machinePassword)
				statusbar.Push(contextID, "Connected")
			}

		} else {
			clremoton.Stop()
			btnSrv.SetLabel("Start")
			machineIDEntry.SetText("")
			machineAuthEntry.SetText("")
			statusbar.Push(contextID, "Stopped")

		}

	})
	controlBox.Add(btnSrv)

	//---
	// CHAT
	//---
	frameChat := gtk.NewFrame("Chat")
	chatBox := gtk.NewVBox(false, 1)
	frameChat.Add(chatBox)

	swinChat := gtk.NewScrolledWindow(nil, nil)
	chatHistory := gtk.NewTextView()
	clremoton.Chat.OnRecv(func(msg string) {
		chatHistoryRecv(chatHistory, msg)
	})

	swinChat.Add(chatHistory)

	chatEntry := gtk.NewEntry()
	chatEntry.Connect("key-press-event", func(ctx *glib.CallbackContext) {
		arg := ctx.Args(0)
		event := *(**gdk.EventKey)(unsafe.Pointer(&arg))
		if event.Keyval == gdk.KEY_Return {
			msgToSend := chatEntry.GetText()
			clremoton.Chat.Send(msgToSend)
			chatHistorySend(chatHistory, msgToSend)
			chatEntry.SetText("")
		}

	})
	chatBox.Add(chatEntry)
	chatBox.Add(swinChat)

	hpaned.Pack1(frameControl, false, false)
	hpaned.Pack2(frameChat, false, true)
	appLayout.Add(statusbar)
	window.Add(appLayout)
	window.ShowAll()
	gtk.Main()
}

func dialogError(win *gtk.Window, err error) {

	log.Error(err)
	dialog := gtk.NewMessageDialog(
		win,
		gtk.DIALOG_MODAL,
		gtk.MESSAGE_ERROR,
		gtk.BUTTONS_CANCEL,
		err.Error(),
	)
	dialog.Response(func() {
		dialog.Destroy()
	})
	dialog.Run()
}

func chatHistorySend(textview *gtk.TextView, msg string) {
	var start gtk.TextIter

	buff := textview.GetBuffer()
	buff.GetStartIter(&start)
	buff.Insert(&start, "< "+msg+"\n")
}

func chatHistoryRecv(textview *gtk.TextView, msg string) {
	var start gtk.TextIter

	buff := textview.GetBuffer()
	buff.GetStartIter(&start)
	buff.Insert(&start, "> "+msg+"\n")
}
