//go:build windows

package gui

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"domain-vpn-router/internal/app"
)

const (
	wmDestroy      = 0x0002
	wmClose        = 0x0010
	wmPaint        = 0x000F
	wmCommand      = 0x0111
	wmUser         = 0x0400
	wmTrayIcon     = wmUser + 1
	wmLButtonDbl   = 0x0203
	wmRButtonUp    = 0x0205
	wsOverlapped   = 0x00000000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsThickFrame   = 0x00040000
	wsMinimizeBox  = 0x00020000
	wsMaximizeBox  = 0x00010000
	wsVisible      = 0x10000000
	cwUseDefault   = 0x80000000
	swHide         = 0
	swShow         = 5
	nimAdd         = 0x00000000
	nimModify      = 0x00000001
	nimDelete      = 0x00000002
	nifMessage     = 0x00000001
	nifIcon        = 0x00000002
	nifTip         = 0x00000004
	idcArrow       = 32512
	idiApplication = 32512
	mfString       = 0x00000000
	mfSeparator    = 0x00000800
	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020
	dtLeft         = 0x00000000
	dtTop          = 0x00000000
	dtWordBreak    = 0x00000010
)

const (
	cmdAuto = 1001 + iota
	cmdTyty
	cmdGlobalProtect
	cmdDirect
	cmdRestoreProxy
	cmdShow
	cmdExit
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	shell32              = syscall.NewLazyDLL("shell32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procInvalidateRect   = user32.NewProc("InvalidateRect")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procLoadIconW        = user32.NewProc("LoadIconW")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32.NewProc("AppendMenuW")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procCheckMenuRadio   = user32.NewProc("CheckMenuRadioItem")
	procSetForeground    = user32.NewProc("SetForegroundWindow")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procBeginPaint       = user32.NewProc("BeginPaint")
	procEndPaint         = user32.NewProc("EndPaint")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procDrawTextW        = user32.NewProc("DrawTextW")
	procSetWindowTextW   = user32.NewProc("SetWindowTextW")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	current *runner
)

type runner struct {
	controller *app.Controller
	ctx        context.Context
	cancel     context.CancelFunc
	hwnd       uintptr
	icon       uintptr
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type point struct {
	X int32
	Y int32
}

type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type paintStruct struct {
	Hdc         uintptr
	Erase       int32
	Paint       rect
	Restore     int32
	IncUpdate   int32
	RGBReserved [32]byte
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type notifyIconData struct {
	Size             uint32
	Hwnd             uintptr
	ID               uint32
	Flags            uint32
	CallbackMessage  uint32
	Icon             uintptr
	Tip              [128]uint16
	State            uint32
	StateMask        uint32
	Info             [256]uint16
	TimeoutOrVersion uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	GuidItem         [16]byte
	BalloonIcon      uintptr
}

func Run(ctx context.Context, controller *app.Controller, showWindow bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := controller.Start(ctx); err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	r := &runner{controller: controller, ctx: runCtx, cancel: cancel}
	current = r

	instance, _, _ := procGetModuleHandleW.Call(0)
	className := utf16Ptr("DomainVPNRouterWindow")
	icon, _, _ := procLoadIconW.Call(0, uintptr(idiApplication))
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   syscall.NewCallback(wndProc),
		Instance:  instance,
		Icon:      icon,
		Cursor:    cursor,
		ClassName: className,
		IconSm:    icon,
	}
	if ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		return fmt.Errorf("注册窗口类失败: %v", err)
	}

	title := utf16Ptr("Domain VPN Router")
	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsThickFrame | wsMinimizeBox | wsMaximizeBox)
	r.hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		style,
		uintptr(cwUseDefault), uintptr(cwUseDefault),
		640, 520,
		0, 0, instance, 0,
	)
	if r.hwnd == 0 {
		return fmt.Errorf("创建窗口失败")
	}
	r.icon = icon
	if err := r.addTrayIcon(); err != nil {
		return err
	}
	if showWindow {
		r.showWindow()
	}

	go func() {
		<-runCtx.Done()
		procDestroyWindow.Call(r.hwnd)
	}()

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	_ = r.deleteTrayIcon()
	shutdownCtx, shutdownCancel := app.ShutdownContext()
	defer shutdownCancel()
	return controller.Shutdown(shutdownCtx)
}

func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	r := current
	switch msg {
	case wmTrayIcon:
		switch uint32(lParam) {
		case wmRButtonUp:
			r.showMenu()
			return 0
		case wmLButtonDbl:
			r.showWindow()
			return 0
		}
	case wmCommand:
		if r != nil {
			r.handleCommand(int(wParam & 0xffff))
		}
		return 0
	case wmPaint:
		if r != nil {
			r.paint()
			return 0
		}
	case wmClose:
		procShowWindow.Call(hwnd, swHide)
		return 0
	case wmDestroy:
		if r != nil {
			_ = r.deleteTrayIcon()
			r.cancel()
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func (r *runner) addTrayIcon() error {
	var nid notifyIconData
	nid.Size = uint32(unsafe.Sizeof(nid))
	nid.Hwnd = r.hwnd
	nid.ID = 1
	nid.Flags = nifMessage | nifIcon | nifTip
	nid.CallbackMessage = wmTrayIcon
	nid.Icon = r.icon
	copy(nid.Tip[:], syscall.StringToUTF16("Domain VPN Router"))
	if ret, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); ret == 0 {
		return fmt.Errorf("添加托盘图标失败: %v", err)
	}
	return nil
}

func (r *runner) deleteTrayIcon() error {
	var nid notifyIconData
	nid.Size = uint32(unsafe.Sizeof(nid))
	nid.Hwnd = r.hwnd
	nid.ID = 1
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	return nil
}

func (r *runner) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	appendMenu(menu, cmdAuto, "自动分流")
	appendMenu(menu, cmdTyty, "强制 Tyty")
	appendMenu(menu, cmdGlobalProtect, "强制 GlobalProtect")
	appendMenu(menu, cmdDirect, "本地直连")
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	appendMenu(menu, cmdRestoreProxy, "恢复系统代理")
	appendMenu(menu, cmdShow, "打开状态窗口")
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	appendMenu(menu, cmdExit, "退出")

	mode := r.controller.Mode()
	checked := cmdAuto
	switch mode {
	case app.ModeTyty:
		checked = cmdTyty
	case app.ModeGlobalProtect:
		checked = cmdGlobalProtect
	case app.ModeDirect:
		checked = cmdDirect
	}
	procCheckMenuRadio.Call(menu, cmdAuto, cmdDirect, uintptr(checked), 0)

	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	procSetForeground.Call(r.hwnd)
	procTrackPopupMenu.Call(menu, tpmRightButton|tpmBottomAlign, uintptr(p.X), uintptr(p.Y), 0, r.hwnd, 0)
	procDestroyMenu.Call(menu)
}

func (r *runner) handleCommand(command int) {
	switch command {
	case cmdAuto:
		r.applyModeAsync(app.ModeAuto)
	case cmdTyty:
		r.applyModeAsync(app.ModeTyty)
	case cmdGlobalProtect:
		r.applyModeAsync(app.ModeGlobalProtect)
	case cmdDirect:
		r.applyModeAsync(app.ModeDirect)
	case cmdRestoreProxy:
		go func() {
			_ = r.controller.RestoreProxy()
			r.invalidate()
		}()
	case cmdShow:
		r.showWindow()
	case cmdExit:
		r.cancel()
		procDestroyWindow.Call(r.hwnd)
	}
}

func (r *runner) applyModeAsync(mode app.Mode) {
	go func() {
		_ = r.controller.ApplyMode(context.Background(), mode)
		r.invalidate()
	}()
}

func (r *runner) showWindow() {
	procSetWindowTextW.Call(r.hwnd, uintptr(unsafe.Pointer(utf16Ptr("Domain VPN Router"))))
	procShowWindow.Call(r.hwnd, swShow)
	procUpdateWindow.Call(r.hwnd)
	r.invalidate()
}

func (r *runner) invalidate() {
	procInvalidateRect.Call(r.hwnd, 0, 1)
}

func (r *runner) paint() {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(r.hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(r.hwnd, uintptr(unsafe.Pointer(&ps)))
	var rc rect
	procGetClientRect.Call(r.hwnd, uintptr(unsafe.Pointer(&rc)))
	rc.Left += 16
	rc.Top += 16
	rc.Right -= 16
	rc.Bottom -= 16
	text := r.controller.StatusText(context.Background())
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(utf16Ptr(text))), ^uintptr(0), uintptr(unsafe.Pointer(&rc)), dtLeft|dtTop|dtWordBreak)
}

func appendMenu(menu uintptr, id uintptr, text string) {
	procAppendMenuW.Call(menu, mfString, id, uintptr(unsafe.Pointer(utf16Ptr(text))))
}

func utf16Ptr(s string) *uint16 {
	s = strings.ReplaceAll(s, "\n", "\r\n")
	return syscall.StringToUTF16Ptr(s)
}
