//go:build windows

package gui

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"domain-vpn-router/internal/app"
)

const (
	wmDestroy      = 0x0002
	wmActivate     = 0x0006
	wmClose        = 0x0010
	wmPaint        = 0x000F
	wmCommand      = 0x0111
	wmTimer        = 0x0113
	wmUser         = 0x0400
	wmTrayIcon     = wmUser + 1
	wmLButtonUp    = 0x0202
	wmLButtonDbl   = 0x0203
	wmRButtonUp    = 0x0205
	wsOverlapped   = 0x00000000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsThickFrame   = 0x00040000
	wsMinimizeBox  = 0x00020000
	wsMaximizeBox  = 0x00010000
	wsVisible      = 0x10000000
	wsPopup        = 0x80000000
	wsExTopmost    = 0x00000008
	wsExToolWindow = 0x00000080
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
	mfGrayed       = 0x00000001
	mfSeparator    = 0x00000800
	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020
	tpmReturnCmd   = 0x0100
	smCxScreen     = 0
	smCyScreen     = 1
	dtLeft         = 0x00000000
	dtCenter       = 0x00000001
	dtRight        = 0x00000002
	dtVCenter      = 0x00000004
	dtTop          = 0x00000000
	dtSingleLine   = 0x00000020
	dtWordBreak    = 0x00000010
	dtEndEllipsis  = 0x00008000
	dtNoPrefix     = 0x00000800
	transparent    = 1
	psSolid        = 0
	fwNormal       = 400
	fwSemiBold     = 600
	fwBold         = 700
	defaultCharset = 1
	cleartype      = 5
	refreshTimerID = 1
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
	gdi32                = syscall.NewLazyDLL("gdi32.dll")
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
	procCreateIcon       = user32.NewProc("CreateIcon")
	procDestroyIcon      = user32.NewProc("DestroyIcon")
	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32.NewProc("AppendMenuW")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procCheckMenuRadio   = user32.NewProc("CheckMenuRadioItem")
	procSetForeground    = user32.NewProc("SetForegroundWindow")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procShellExecuteW    = shell32.NewProc("ShellExecuteW")
	procBeginPaint       = user32.NewProc("BeginPaint")
	procEndPaint         = user32.NewProc("EndPaint")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procDrawTextW        = user32.NewProc("DrawTextW")
	procSetTimer         = user32.NewProc("SetTimer")
	procKillTimer        = user32.NewProc("KillTimer")
	procSetWindowTextW   = user32.NewProc("SetWindowTextW")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procCreatePen        = gdi32.NewProc("CreatePen")
	procCreateFontW      = gdi32.NewProc("CreateFontW")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procFillRect         = user32.NewProc("FillRect")
	procRoundRect        = gdi32.NewProc("RoundRect")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procSetTextColor     = gdi32.NewProc("SetTextColor")

	current *runner
)

const (
	viewStatus = iota
	viewLogs
	viewSettings
)

const (
	hitTabStatus = iota + 1
	hitTabLogs
	hitTabSettings
	hitModeAuto
	hitModeTyty
	hitModeGlobalProtect
	hitModeDirect
	hitUseCurrentIP
	hitClearDirectIP
	hitSaveSettings
	hitRestoreProxy
	hitOpenConfig
)

type runner struct {
	controller     *app.Controller
	ctx            context.Context
	cancel         context.CancelFunc
	hwnd           uintptr
	icon           uintptr
	iconOwned      bool
	menuHwnd       uintptr
	menuHits       []hitTarget
	statusMu       sync.RWMutex
	status         app.Status
	view           int
	hits           []hitTarget
	settingsBindIP string
	notice         string
}

type hitTarget struct {
	Rect   rect
	Action int
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
	r := &runner{
		controller:     controller,
		ctx:            runCtx,
		cancel:         cancel,
		status:         controller.StatusSnapshot(),
		settingsBindIP: controller.DirectBindIP(),
	}
	current = r

	instance, _, _ := procGetModuleHandleW.Call(0)
	className := utf16Ptr("DomainVPNRouterWindow")
	menuClassName := utf16Ptr("DomainVPNRouterMenu")
	icon := createAppIcon(instance)
	iconOwned := icon != 0
	if icon == 0 {
		icon, _, _ = procLoadIconW.Call(0, uintptr(idiApplication))
	}
	r.iconOwned = iconOwned
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
	menuWC := wc
	menuWC.WndProc = syscall.NewCallback(menuWndProc)
	menuWC.ClassName = menuClassName
	if ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&menuWC))); ret == 0 {
		return fmt.Errorf("注册菜单窗口类失败: %v", err)
	}

	title := utf16Ptr("Domain VPN Router")
	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsThickFrame | wsMinimizeBox | wsMaximizeBox)
	r.hwnd, _, _ = procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		style,
		uintptr(cwUseDefault), uintptr(cwUseDefault),
		860, 620,
		0, 0, instance, 0,
	)
	if r.hwnd == 0 {
		return fmt.Errorf("创建窗口失败")
	}
	r.icon = icon
	trayReady := r.addTrayIcon() == nil
	procSetTimer.Call(r.hwnd, refreshTimerID, 1000, 0)
	if showWindow {
		r.showWindow()
	} else if !trayReady {
		r.showWindow()
	}

	go func() {
		<-runCtx.Done()
		procDestroyWindow.Call(r.hwnd)
	}()
	r.startStatusRefresh()

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
	case wmLButtonUp:
		if r != nil {
			r.handleClick(pointFromLParam(lParam))
			return 0
		}
	case wmPaint:
		if r != nil {
			r.paint()
			return 0
		}
	case wmTimer:
		if r != nil && wParam == refreshTimerID {
			r.invalidate()
			return 0
		}
	case wmClose:
		procShowWindow.Call(hwnd, swHide)
		return 0
	case wmDestroy:
		if r != nil {
			procKillTimer.Call(hwnd, refreshTimerID)
			_ = r.deleteTrayIcon()
			if r.iconOwned && r.icon != 0 {
				procDestroyIcon.Call(r.icon)
			}
			r.cancel()
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func menuWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	r := current
	switch msg {
	case wmActivate:
		if r != nil && int(wParam&0xffff) == 0 {
			r.closeMenu()
			return 0
		}
	case wmPaint:
		if r != nil {
			r.paintTrayMenu(hwnd)
			return 0
		}
	case wmLButtonUp:
		if r != nil {
			r.handleMenuClick(pointFromLParam(lParam))
			return 0
		}
	case wmClose:
		if r != nil {
			r.closeMenu()
			return 0
		}
	case wmDestroy:
		if r != nil && r.menuHwnd == hwnd {
			r.menuHwnd = 0
			r.menuHits = nil
		}
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
	if r.menuHwnd != 0 {
		r.closeMenu()
	}
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))

	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	status := r.currentStatus()
	appendMenu(menu, mfString|mfGrayed, 0, "当前模式: "+modeTitle(status.Mode))
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, cmdAuto, "自动分流")
	appendMenu(menu, mfString, cmdTyty, "强制 Tyty")
	appendMenu(menu, mfString, cmdGlobalProtect, "强制 GlobalProtect")
	appendMenu(menu, mfString, cmdDirect, "本地直连")
	procCheckMenuRadio.Call(menu, cmdAuto, cmdDirect, modeCommand(status.Mode), 0)
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, cmdShow, "打开主窗口")
	appendMenu(menu, mfString, cmdRestoreProxy, "恢复系统代理")
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, cmdExit, "退出")

	procSetForeground.Call(r.hwnd)
	command, _, _ := procTrackPopupMenu.Call(
		menu,
		uintptr(tpmRightButton|tpmBottomAlign|tpmReturnCmd),
		uintptr(p.X), uintptr(p.Y),
		0, r.hwnd, 0,
	)
	if command != 0 {
		r.handleCommand(int(command))
	}
}

func (r *runner) closeMenu() {
	if r.menuHwnd != 0 {
		procDestroyWindow.Call(r.menuHwnd)
		r.menuHwnd = 0
	}
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

func (r *runner) paintTrayMenu(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var rc rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	status := r.currentStatus()
	r.menuHits = nil

	fillRect(hdc, rc, rgb(224, 231, 239))
	procSetBkMode.Call(hdc, transparent)
	panel := rect{Left: rc.Left + 5, Top: rc.Top + 5, Right: rc.Right - 5, Bottom: rc.Bottom - 5}
	drawRoundedFill(hdc, panel, rgb(255, 255, 255), rgb(148, 163, 184), 12)
	drawText(hdc, "Domain VPN Router", rect{Left: panel.Left + 14, Top: panel.Top + 12, Right: panel.Right - 14, Bottom: panel.Top + 34}, 13, fwBold, rgb(15, 23, 42), dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, modeTitle(status.Mode), rect{Left: panel.Left + 14, Top: panel.Top + 34, Right: panel.Right - 14, Bottom: panel.Top + 52}, 10, fwSemiBold, modeColor(status.Mode), dtLeft|dtSingleLine|dtNoPrefix)

	top := panel.Top + 62
	r.drawTrayMenuItem(hdc, rect{Left: panel.Left + 8, Top: top, Right: panel.Right - 8, Bottom: top + 28}, "自动分流", "AUTO", status.Mode == app.ModeAuto, cmdAuto)
	r.drawTrayMenuItem(hdc, rect{Left: panel.Left + 8, Top: top + 31, Right: panel.Right - 8, Bottom: top + 59}, "强制 Tyty", "TY", status.Mode == app.ModeTyty, cmdTyty)
	r.drawTrayMenuItem(hdc, rect{Left: panel.Left + 8, Top: top + 62, Right: panel.Right - 8, Bottom: top + 90}, "强制 GlobalProtect", "GP", status.Mode == app.ModeGlobalProtect, cmdGlobalProtect)
	r.drawTrayMenuItem(hdc, rect{Left: panel.Left + 8, Top: top + 93, Right: panel.Right - 8, Bottom: top + 121}, "本地直连", "DIR", status.Mode == app.ModeDirect, cmdDirect)

	fillRect(hdc, rect{Left: panel.Left + 12, Top: top + 132, Right: panel.Right - 12, Bottom: top + 133}, rgb(226, 232, 240))
	r.drawTrayMenuItem(hdc, rect{Left: panel.Left + 8, Top: top + 142, Right: panel.Right - 8, Bottom: top + 170}, "打开主窗口", "OPEN", false, cmdShow)
	r.drawTrayMenuItem(hdc, rect{Left: panel.Left + 8, Top: top + 173, Right: panel.Right - 8, Bottom: top + 201}, "恢复系统代理", "FIX", false, cmdRestoreProxy)
	r.drawTrayMenuItem(hdc, rect{Left: panel.Left + 8, Top: top + 204, Right: panel.Right - 8, Bottom: top + 232}, "退出", "EXIT", false, cmdExit)
}

func (r *runner) drawTrayMenuItem(hdc uintptr, rc rect, label, tag string, active bool, command int) {
	bg := rgb(255, 255, 255)
	fg := rgb(15, 23, 42)
	tagColor := rgb(100, 116, 139)
	if active {
		bg = rgb(236, 253, 245)
		fg = rgb(4, 120, 87)
		tagColor = rgb(5, 150, 105)
	}
	drawRoundedFill(hdc, rc, bg, bg, 6)
	drawText(hdc, tag, rect{Left: rc.Left + 10, Top: rc.Top + 7, Right: rc.Left + 52, Bottom: rc.Bottom - 5}, 9, fwBold, tagColor, dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, label, rect{Left: rc.Left + 58, Top: rc.Top + 5, Right: rc.Right - 10, Bottom: rc.Bottom - 5}, 12, fwSemiBold, fg, dtLeft|dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	r.menuHits = append(r.menuHits, hitTarget{Rect: rc, Action: command})
}

func (r *runner) handleMenuClick(p point) {
	for i := len(r.menuHits) - 1; i >= 0; i-- {
		hit := r.menuHits[i]
		if !pointInRect(p, hit.Rect) {
			continue
		}
		command := hit.Action
		r.closeMenu()
		r.handleCommand(command)
		return
	}
}

func (r *runner) applyModeAsync(mode app.Mode) {
	go func() {
		_ = r.controller.ApplyMode(context.Background(), mode)
		r.invalidate()
	}()
}

func (r *runner) handleClick(p point) {
	for i := len(r.hits) - 1; i >= 0; i-- {
		hit := r.hits[i]
		if !pointInRect(p, hit.Rect) {
			continue
		}
		switch hit.Action {
		case hitTabStatus:
			r.view = viewStatus
		case hitTabLogs:
			r.view = viewLogs
		case hitTabSettings:
			r.view = viewSettings
			r.settingsBindIP = r.controller.DirectBindIP()
		case hitModeAuto:
			r.applyModeAsync(app.ModeAuto)
		case hitModeTyty:
			r.applyModeAsync(app.ModeTyty)
		case hitModeGlobalProtect:
			r.applyModeAsync(app.ModeGlobalProtect)
		case hitModeDirect:
			r.applyModeAsync(app.ModeDirect)
		case hitUseCurrentIP:
			if ip := firstPhysicalIPv4(); ip != "" {
				r.settingsBindIP = ip
				r.notice = "已填入当前本机 IPv4，点击保存后生效"
			} else {
				r.notice = "没有找到可用的本机 IPv4"
			}
		case hitClearDirectIP:
			r.settingsBindIP = ""
			r.notice = "已清空，点击保存后使用系统默认路由"
		case hitSaveSettings:
			if err := r.controller.UpdateDirectBindIP(r.settingsBindIP); err != nil {
				r.notice = "保存失败: " + err.Error()
			} else {
				r.notice = "设置已保存并立即生效"
			}
		case hitRestoreProxy:
			go func() {
				if err := r.controller.RestoreProxy(); err != nil {
					r.notice = "恢复失败: " + err.Error()
				} else {
					r.notice = "系统代理已恢复"
				}
				r.invalidate()
			}()
		case hitOpenConfig:
			r.openConfig()
		}
		r.invalidate()
		return
	}
}

func (r *runner) openConfig() {
	procShellExecuteW.Call(
		r.hwnd,
		uintptr(unsafe.Pointer(utf16Ptr("open"))),
		uintptr(unsafe.Pointer(utf16Ptr("config.yaml"))),
		0,
		0,
		swShow,
	)
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

func (r *runner) startStatusRefresh() {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.ctx.Done():
				return
			case <-ticker.C:
				r.updateStatus(r.controller.StatusSnapshot(), true)
				r.invalidate()
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		r.updateStatus(r.controller.Status(context.Background()), false)
		r.invalidate()
		for {
			select {
			case <-r.ctx.Done():
				return
			case <-ticker.C:
				r.updateStatus(r.controller.Status(context.Background()), false)
				r.invalidate()
			}
		}
	}()
}

func (r *runner) updateStatus(status app.Status, keepVPN bool) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	if keepVPN {
		status.TytyUp = r.status.TytyUp
		status.GlobalUp = r.status.GlobalUp
	}
	r.status = status
}

func (r *runner) currentStatus() app.Status {
	r.statusMu.RLock()
	defer r.statusMu.RUnlock()
	status := r.status
	status.Logs = append([]string(nil), r.status.Logs...)
	return status
}

func (r *runner) paint() {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(r.hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(r.hwnd, uintptr(unsafe.Pointer(&ps)))
	var rc rect
	procGetClientRect.Call(r.hwnd, uintptr(unsafe.Pointer(&rc)))
	status := r.currentStatus()
	r.hits = nil

	fillRect(hdc, rc, rgb(245, 247, 251))
	procSetBkMode.Call(hdc, transparent)

	sidebar := rect{Left: rc.Left, Top: rc.Top, Right: rc.Left + 196, Bottom: rc.Bottom}
	fillRect(hdc, sidebar, rgb(255, 255, 255))
	fillRect(hdc, rect{Left: sidebar.Right - 1, Top: sidebar.Top, Right: sidebar.Right, Bottom: sidebar.Bottom}, rgb(226, 232, 240))
	drawCircleBadge(hdc, sidebar.Left+24, sidebar.Top+28, 42, "D", rgb(16, 185, 129), rgb(255, 255, 255), 18)
	drawText(hdc, "Domain VPN", rect{Left: sidebar.Left + 78, Top: sidebar.Top + 28, Right: sidebar.Right - 18, Bottom: sidebar.Top + 54}, 17, fwBold, rgb(15, 23, 42), dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, "Router", rect{Left: sidebar.Left + 78, Top: sidebar.Top + 54, Right: sidebar.Right - 18, Bottom: sidebar.Top + 76}, 13, fwSemiBold, rgb(100, 116, 139), dtLeft|dtSingleLine|dtNoPrefix)
	r.drawNavItem(hdc, rect{Left: sidebar.Left + 16, Top: sidebar.Top + 118, Right: sidebar.Right - 16, Bottom: sidebar.Top + 160}, "", "状态", r.view == viewStatus, hitTabStatus)
	r.drawNavItem(hdc, rect{Left: sidebar.Left + 16, Top: sidebar.Top + 168, Right: sidebar.Right - 16, Bottom: sidebar.Top + 210}, "", "日志", r.view == viewLogs, hitTabLogs)
	r.drawNavItem(hdc, rect{Left: sidebar.Left + 16, Top: sidebar.Top + 218, Right: sidebar.Right - 16, Bottom: sidebar.Top + 260}, "", "设置", r.view == viewSettings, hitTabSettings)
	drawText(hdc, "系统代理", rect{Left: sidebar.Left + 20, Top: sidebar.Bottom - 92, Right: sidebar.Right - 20, Bottom: sidebar.Bottom - 68}, 12, fwSemiBold, rgb(100, 116, 139), dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, boolText(status.SystemProxyOn), rect{Left: sidebar.Left + 20, Top: sidebar.Bottom - 62, Right: sidebar.Right - 20, Bottom: sidebar.Bottom - 34}, 18, fwBold, statusColor(status.SystemProxyOn), dtLeft|dtSingleLine|dtNoPrefix)

	main := rect{Left: sidebar.Right, Top: rc.Top, Right: rc.Right, Bottom: rc.Bottom}
	header := rect{Left: main.Left, Top: main.Top, Right: main.Right, Bottom: main.Top + 98}
	fillRect(hdc, header, rgb(255, 255, 255))
	title := "状态总览"
	subtitle := "点击模式按钮即可切换网络策略"
	if r.view == viewLogs {
		title = "实时日志"
		subtitle = "查看最近访问、路由动作和错误信息"
	} else if r.view == viewSettings {
		title = "设置"
		subtitle = "调整直连绑定 IP，保存后立即生效"
	}
	drawText(hdc, title, rect{Left: main.Left + 28, Top: main.Top + 24, Right: main.Right - 210, Bottom: main.Top + 56}, 24, fwBold, rgb(15, 23, 42), dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, subtitle, rect{Left: main.Left + 30, Top: main.Top + 60, Right: main.Right - 210, Bottom: main.Top + 82}, 13, fwNormal, rgb(100, 116, 139), dtLeft|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	drawPill(hdc, rect{Left: main.Right - 182, Top: main.Top + 30, Right: main.Right - 28, Bottom: main.Top + 66}, modeTitle(status.Mode), modeColor(status.Mode))

	content := rect{Left: main.Left + 28, Top: main.Top + 122, Right: main.Right - 28, Bottom: main.Bottom - 24}
	if r.view == viewSettings {
		r.drawSettingsPage(hdc, content, status)
	} else if r.view == viewLogs {
		r.drawLogsPage(hdc, content, status)
	} else {
		r.drawStatusPage(hdc, content, status)
	}
}

func (r *runner) drawStatusPage(hdc uintptr, rc rect, status app.Status) {
	gap := int32(14)
	cardWidth := (rc.Right - rc.Left - gap*2) / 3
	drawMetricCard(hdc, rect{Left: rc.Left, Top: rc.Top, Right: rc.Left + cardWidth, Bottom: rc.Top + 96}, "代理监听", status.ProxyListen, "NET", status.ProxyRunning)
	drawMetricCard(hdc, rect{Left: rc.Left + cardWidth + gap, Top: rc.Top, Right: rc.Left + cardWidth*2 + gap, Bottom: rc.Top + 96}, "直连绑定", emptyText(status.DirectBindIP, "自动"), "IP", status.DirectBindIP != "")
	drawMetricCard(hdc, rect{Left: rc.Left + cardWidth*2 + gap*2, Top: rc.Top, Right: rc.Right, Bottom: rc.Top + 96}, "系统代理", boolText(status.SystemProxyOn), "SYS", status.SystemProxyOn)

	modeTop := rc.Top + 118
	buttonWidth := (rc.Right - rc.Left - gap*3) / 4
	r.drawModeButton(hdc, rect{Left: rc.Left, Top: modeTop, Right: rc.Left + buttonWidth, Bottom: modeTop + 86}, "AUTO", "自动分流", "按域名规则选择", status.Mode == app.ModeAuto, hitModeAuto)
	r.drawModeButton(hdc, rect{Left: rc.Left + (buttonWidth+gap)*1, Top: modeTop, Right: rc.Left + (buttonWidth+gap)*1 + buttonWidth, Bottom: modeTop + 86}, "TY", "强制 Tyty", "全部公网走 Tyty", status.Mode == app.ModeTyty, hitModeTyty)
	r.drawModeButton(hdc, rect{Left: rc.Left + (buttonWidth+gap)*2, Top: modeTop, Right: rc.Left + (buttonWidth+gap)*2 + buttonWidth, Bottom: modeTop + 86}, "GP", "强制 GP", "全部公网走公司 VPN", status.Mode == app.ModeGlobalProtect, hitModeGlobalProtect)
	r.drawModeButton(hdc, rect{Left: rc.Left + (buttonWidth+gap)*3, Top: modeTop, Right: rc.Right, Bottom: modeTop + 86}, "DIR", "本地直连", "关闭系统代理", status.Mode == app.ModeDirect, hitModeDirect)

	vpnTop := modeTop + 106
	vpnWidth := (rc.Right - rc.Left - gap) / 2
	drawVPNCard(hdc, rect{Left: rc.Left, Top: vpnTop, Right: rc.Left + vpnWidth, Bottom: vpnTop + 82}, "Tyty", status.TytyUp, "TY")
	drawVPNCard(hdc, rect{Left: rc.Left + vpnWidth + gap, Top: vpnTop, Right: rc.Right, Bottom: vpnTop + 82}, "GlobalProtect", status.GlobalUp, "GP")

	if status.LastError != "" {
		drawAlert(hdc, rect{Left: rc.Left, Top: vpnTop + 102, Right: rc.Right, Bottom: vpnTop + 154}, status.LastError)
	}
}

func (r *runner) drawLogsPage(hdc uintptr, rc rect, status app.Status) {
	logTop := rc.Top
	if status.LastError != "" {
		drawAlert(hdc, rect{Left: rc.Left, Top: logTop, Right: rc.Right, Bottom: logTop + 52}, status.LastError)
		logTop += 66
	}
	drawLogPanel(hdc, rect{Left: rc.Left, Top: logTop, Right: rc.Right, Bottom: rc.Bottom}, status.Logs)
}

func (r *runner) drawSettingsPage(hdc uintptr, rc rect, status app.Status) {
	drawCard(hdc, rect{Left: rc.Left, Top: rc.Top, Right: rc.Right, Bottom: rc.Top + 172})
	drawText(hdc, "直连绑定 IP", rect{Left: rc.Left + 22, Top: rc.Top + 20, Right: rc.Right - 22, Bottom: rc.Top + 48}, 18, fwBold, rgb(15, 23, 42), dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, "用于 direct 流量指定本地网卡。留空会使用系统默认路由。", rect{Left: rc.Left + 22, Top: rc.Top + 52, Right: rc.Right - 22, Bottom: rc.Top + 76}, 13, fwNormal, rgb(100, 116, 139), dtLeft|dtSingleLine|dtNoPrefix)
	field := rect{Left: rc.Left + 22, Top: rc.Top + 90, Right: rc.Right - 22, Bottom: rc.Top + 132}
	drawRoundedFill(hdc, field, rgb(248, 250, 252), rgb(203, 213, 225), 12)
	drawText(hdc, emptyText(r.settingsBindIP, "自动选择系统默认路由"), rect{Left: field.Left + 14, Top: field.Top + 10, Right: field.Right - 14, Bottom: field.Bottom - 8}, 16, fwSemiBold, rgb(15, 23, 42), dtLeft|dtSingleLine|dtEndEllipsis|dtNoPrefix)

	buttonTop := rc.Top + 196
	buttonW := int32(188)
	r.drawActionButton(hdc, rect{Left: rc.Left, Top: buttonTop, Right: rc.Left + buttonW, Bottom: buttonTop + 48}, "使用当前 WLAN IP", rgb(37, 99, 235), hitUseCurrentIP)
	r.drawActionButton(hdc, rect{Left: rc.Left + buttonW + 14, Top: buttonTop, Right: rc.Left + buttonW*2 + 14, Bottom: buttonTop + 48}, "清空绑定", rgb(100, 116, 139), hitClearDirectIP)
	r.drawActionButton(hdc, rect{Left: rc.Left + (buttonW+14)*2, Top: buttonTop, Right: rc.Left + buttonW*3 + 28, Bottom: buttonTop + 48}, "保存设置", rgb(5, 150, 105), hitSaveSettings)

	drawCard(hdc, rect{Left: rc.Left, Top: buttonTop + 78, Right: rc.Right, Bottom: buttonTop + 190})
	drawText(hdc, "维护操作", rect{Left: rc.Left + 22, Top: buttonTop + 98, Right: rc.Right - 22, Bottom: buttonTop + 126}, 18, fwBold, rgb(15, 23, 42), dtLeft|dtSingleLine|dtNoPrefix)
	r.drawSecondaryButton(hdc, rect{Left: rc.Left + 22, Top: buttonTop + 138, Right: rc.Left + 194, Bottom: buttonTop + 174}, "恢复系统代理", hitRestoreProxy)
	r.drawSecondaryButton(hdc, rect{Left: rc.Left + 208, Top: buttonTop + 138, Right: rc.Left + 380, Bottom: buttonTop + 174}, "打开配置文件", hitOpenConfig)

	infoTop := buttonTop + 220
	drawText(hdc, "当前配置", rect{Left: rc.Left, Top: infoTop, Right: rc.Right, Bottom: infoTop + 26}, 16, fwBold, rgb(15, 23, 42), dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, "代理监听: "+status.ProxyListen, rect{Left: rc.Left, Top: infoTop + 34, Right: rc.Right, Bottom: infoTop + 58}, 13, fwNormal, rgb(71, 85, 105), dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, "已保存直连绑定: "+emptyText(status.DirectBindIP, "自动"), rect{Left: rc.Left, Top: infoTop + 62, Right: rc.Right, Bottom: infoTop + 86}, 13, fwNormal, rgb(71, 85, 105), dtLeft|dtSingleLine|dtNoPrefix)
	if r.notice != "" {
		drawText(hdc, r.notice, rect{Left: rc.Left, Top: rc.Bottom - 30, Right: rc.Right, Bottom: rc.Bottom}, 13, fwSemiBold, rgb(5, 150, 105), dtLeft|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	}
}

func appendMenu(menu uintptr, flags uintptr, id uintptr, text string) {
	var label uintptr
	if text != "" {
		label = uintptr(unsafe.Pointer(utf16Ptr(text)))
	}
	procAppendMenuW.Call(menu, flags, id, label)
}

func modeCommand(mode app.Mode) uintptr {
	switch mode {
	case app.ModeTyty:
		return cmdTyty
	case app.ModeGlobalProtect:
		return cmdGlobalProtect
	case app.ModeDirect:
		return cmdDirect
	default:
		return cmdAuto
	}
}

func (r *runner) addHit(rc rect, action int) {
	r.hits = append(r.hits, hitTarget{Rect: rc, Action: action})
}

func (r *runner) drawNavItem(hdc uintptr, rc rect, icon, label string, active bool, action int) {
	bg := rgb(255, 255, 255)
	fg := rgb(71, 85, 105)
	if active {
		bg = rgb(236, 253, 245)
		fg = rgb(4, 120, 87)
	}
	drawRoundedFill(hdc, rc, bg, bg, 8)
	if active {
		fillRounded(hdc, rect{Left: rc.Left, Top: rc.Top + 8, Right: rc.Left + 4, Bottom: rc.Bottom - 8}, rgb(16, 185, 129), 2)
	}
	drawText(hdc, label, rect{Left: rc.Left + 18, Top: rc.Top + 10, Right: rc.Right - 16, Bottom: rc.Bottom - 10}, 15, fwSemiBold, fg, dtLeft|dtVCenter|dtSingleLine|dtNoPrefix)
	r.addHit(rc, action)
}

func (r *runner) drawModeButton(hdc uintptr, rc rect, icon, title, subtitle string, active bool, action int) {
	fill := rgb(255, 255, 255)
	stroke := rgb(226, 232, 240)
	titleColor := rgb(15, 23, 42)
	if active {
		fill = rgb(236, 253, 245)
		stroke = rgb(16, 185, 129)
		titleColor = rgb(4, 120, 87)
	}
	drawRoundedFill(hdc, rc, fill, stroke, 8)
	drawText(hdc, icon, rect{Left: rc.Left + 16, Top: rc.Top + 16, Right: rc.Left + 58, Bottom: rc.Top + 40}, 13, fwBold, titleColor, dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, title, rect{Left: rc.Left + 16, Top: rc.Top + 40, Right: rc.Right - 14, Bottom: rc.Top + 62}, 15, fwBold, titleColor, dtLeft|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	drawText(hdc, subtitle, rect{Left: rc.Left + 16, Top: rc.Top + 62, Right: rc.Right - 14, Bottom: rc.Top + 80}, 11, fwNormal, rgb(100, 116, 139), dtLeft|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	r.addHit(rc, action)
}

func (r *runner) drawActionButton(hdc uintptr, rc rect, text string, color uintptr, action int) {
	drawRoundedFill(hdc, rc, color, color, 8)
	drawText(hdc, text, rc, 14, fwBold, rgb(255, 255, 255), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	r.addHit(rc, action)
}

func (r *runner) drawSecondaryButton(hdc uintptr, rc rect, text string, action int) {
	drawRoundedFill(hdc, rc, rgb(248, 250, 252), rgb(203, 213, 225), 8)
	drawText(hdc, text, rc, 13, fwSemiBold, rgb(15, 23, 42), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	r.addHit(rc, action)
}

func drawMetricCard(hdc uintptr, rc rect, title, value, icon string, ok bool) {
	drawCard(hdc, rc)
	drawText(hdc, icon, rect{Left: rc.Left + 18, Top: rc.Top + 18, Right: rc.Left + 62, Bottom: rc.Top + 40}, 12, fwBold, rgb(16, 185, 129), dtLeft|dtSingleLine|dtNoPrefix)
	drawMiniStatus(hdc, rect{Left: rc.Right - 78, Top: rc.Top + 16, Right: rc.Right - 16, Bottom: rc.Top + 38}, ok)
	drawText(hdc, title, rect{Left: rc.Left + 18, Top: rc.Top + 42, Right: rc.Right - 18, Bottom: rc.Top + 64}, 13, fwSemiBold, rgb(100, 116, 139), dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, value, rect{Left: rc.Left + 18, Top: rc.Top + 66, Right: rc.Right - 18, Bottom: rc.Top + 90}, 17, fwBold, rgb(15, 23, 42), dtLeft|dtSingleLine|dtEndEllipsis|dtNoPrefix)
}

func drawVPNCard(hdc uintptr, rc rect, name string, up bool, icon string) {
	drawCard(hdc, rc)
	drawCircleBadge(hdc, rc.Left+18, rc.Top+18, 44, icon, statusSoftColor(up), statusColor(up), 13)
	drawText(hdc, name, rect{Left: rc.Left + 82, Top: rc.Top + 20, Right: rc.Right - 20, Bottom: rc.Top + 46}, 18, fwBold, rgb(15, 23, 42), dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, statusLine(up), rect{Left: rc.Left + 82, Top: rc.Top + 48, Right: rc.Right - 20, Bottom: rc.Top + 72}, 13, fwSemiBold, statusColor(up), dtLeft|dtSingleLine|dtNoPrefix)
}

func drawAlert(hdc uintptr, rc rect, message string) {
	drawRoundedFill(hdc, rc, rgb(254, 242, 242), rgb(254, 202, 202), 12)
	drawText(hdc, "!", rect{Left: rc.Left + 18, Top: rc.Top + 12, Right: rc.Left + 48, Bottom: rc.Bottom - 10}, 20, fwBold, rgb(220, 38, 38), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	drawText(hdc, "最近错误: "+message, rect{Left: rc.Left + 56, Top: rc.Top + 16, Right: rc.Right - 18, Bottom: rc.Bottom - 12}, 14, fwSemiBold, rgb(153, 27, 27), dtLeft|dtSingleLine|dtEndEllipsis|dtNoPrefix)
}

func drawLogPanel(hdc uintptr, rc rect, logs []string) {
	drawCard(hdc, rc)
	drawText(hdc, "实时日志", rect{Left: rc.Left + 18, Top: rc.Top + 16, Right: rc.Right - 150, Bottom: rc.Top + 42}, 18, fwBold, rgb(15, 23, 42), dtLeft|dtSingleLine|dtNoPrefix)
	drawText(hdc, "每秒自动刷新", rect{Left: rc.Right - 150, Top: rc.Top + 18, Right: rc.Right - 18, Bottom: rc.Top + 40}, 12, fwSemiBold, rgb(34, 197, 94), dtRight|dtSingleLine|dtNoPrefix)

	list := logs
	maxLines := int((rc.Bottom - rc.Top - 66) / 24)
	if maxLines < 1 {
		maxLines = 1
	}
	if len(list) > maxLines {
		list = list[len(list)-maxLines:]
	}
	if len(list) == 0 {
		drawText(hdc, "暂无日志，等待新的访问请求。", rect{Left: rc.Left + 18, Top: rc.Top + 58, Right: rc.Right - 18, Bottom: rc.Top + 86}, 14, fwNormal, rgb(100, 116, 139), dtLeft|dtSingleLine|dtNoPrefix)
		return
	}
	top := rc.Top + 54
	for i, line := range list {
		row := rect{Left: rc.Left + 14, Top: top + int32(i*24), Right: rc.Right - 14, Bottom: top + int32(i*24) + 22}
		if i%2 == 0 {
			fillRounded(hdc, row, rgb(248, 250, 252), 8)
		}
		drawText(hdc, line, rect{Left: row.Left + 10, Top: row.Top + 2, Right: row.Right - 10, Bottom: row.Bottom}, 12, fwNormal, rgb(51, 65, 85), dtLeft|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	}
}

func drawCard(hdc uintptr, rc rect) {
	drawRoundedFill(hdc, rc, rgb(255, 255, 255), rgb(226, 232, 240), 8)
}

func drawPill(hdc uintptr, rc rect, text string, color uintptr) {
	drawRoundedFill(hdc, rc, color, color, 8)
	drawText(hdc, text, rc, 13, fwBold, rgb(255, 255, 255), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
}

func drawStatusDot(hdc uintptr, x, y int32, ok bool) {
	color := statusColor(ok)
	fillRounded(hdc, rect{Left: x, Top: y, Right: x + 12, Bottom: y + 12}, color, 6)
}

func drawMiniStatus(hdc uintptr, rc rect, ok bool) {
	bg := rgb(220, 252, 231)
	fg := rgb(22, 101, 52)
	if !ok {
		bg = rgb(241, 245, 249)
		fg = rgb(100, 116, 139)
	}
	drawRoundedFill(hdc, rc, bg, bg, 8)
	drawText(hdc, statusText(ok), rc, 10, fwSemiBold, fg, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
}

func drawCircleBadge(hdc uintptr, left, top, size int32, text string, bg, fg uintptr, fontSize int32) {
	fillRounded(hdc, rect{Left: left, Top: top, Right: left + size, Bottom: top + size}, bg, size/2)
	drawText(hdc, text, rect{Left: left, Top: top, Right: left + size, Bottom: top + size}, fontSize, fwBold, fg, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
}

func drawRoundedFill(hdc uintptr, rc rect, fill, stroke uintptr, radius int32) {
	brush, _, _ := procCreateSolidBrush.Call(fill)
	pen, _, _ := procCreatePen.Call(psSolid, 1, stroke)
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	procRoundRect.Call(hdc, uintptr(rc.Left), uintptr(rc.Top), uintptr(rc.Right), uintptr(rc.Bottom), uintptr(radius), uintptr(radius))
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)
}

func fillRounded(hdc uintptr, rc rect, color uintptr, radius int32) {
	drawRoundedFill(hdc, rc, color, color, radius)
}

func fillRect(hdc uintptr, rc rect, color uintptr) {
	brush, _, _ := procCreateSolidBrush.Call(color)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
	procDeleteObject.Call(brush)
}

func drawText(hdc uintptr, text string, rc rect, size int32, weight uintptr, color uintptr, flags uintptr) {
	font := createFont(size, weight)
	oldFont, _, _ := procSelectObject.Call(hdc, font)
	procSetTextColor.Call(hdc, color)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(utf16Ptr(text))), ^uintptr(0), uintptr(unsafe.Pointer(&rc)), flags)
	procSelectObject.Call(hdc, oldFont)
	procDeleteObject.Call(font)
}

func createFont(size int32, weight uintptr) uintptr {
	fontName := utf16Ptr("Microsoft YaHei UI")
	font, _, _ := procCreateFontW.Call(
		uintptr(-size), 0, 0, 0, weight, 0, 0, 0,
		defaultCharset, 0, 0, cleartype, 0,
		uintptr(unsafe.Pointer(fontName)),
	)
	return font
}

func rgb(r, g, b byte) uintptr {
	return uintptr(uint32(r) | uint32(g)<<8 | uint32(b)<<16)
}

func statusText(ok bool) string {
	if ok {
		return "运行中"
	}
	return "未启用"
}

func statusLine(ok bool) string {
	if ok {
		return "网卡已连接"
	}
	return "等待连接"
}

func statusColor(ok bool) uintptr {
	if ok {
		return rgb(22, 163, 74)
	}
	return rgb(239, 68, 68)
}

func statusSoftColor(ok bool) uintptr {
	if ok {
		return rgb(220, 252, 231)
	}
	return rgb(254, 226, 226)
}

func boolText(ok bool) string {
	if ok {
		return "已启用"
	}
	return "已关闭"
}

func modeTitle(mode app.Mode) string {
	switch mode {
	case app.ModeTyty:
		return "强制 Tyty"
	case app.ModeGlobalProtect:
		return "强制 GlobalProtect"
	case app.ModeDirect:
		return "本地直连"
	default:
		return "自动分流"
	}
}

func modeColor(mode app.Mode) uintptr {
	switch mode {
	case app.ModeTyty:
		return rgb(37, 99, 235)
	case app.ModeGlobalProtect:
		return rgb(124, 58, 237)
	case app.ModeDirect:
		return rgb(100, 116, 139)
	default:
		return rgb(5, 150, 105)
	}
}

func emptyText(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func pointInRect(p point, rc rect) bool {
	return p.X >= rc.Left && p.X <= rc.Right && p.Y >= rc.Top && p.Y <= rc.Bottom
}

func pointFromLParam(lParam uintptr) point {
	return point{
		X: int32(int16(lParam & 0xffff)),
		Y: int32(int16((lParam >> 16) & 0xffff)),
	}
}

func firstPhysicalIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(iface.Name + " " + iface.HardwareAddr.String())
		if strings.Contains(name, "mihomo") || strings.Contains(name, "wsl") || strings.Contains(name, "vethernet") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil || ip.To4() == nil {
				continue
			}
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			return ip.String()
		}
	}
	return ""
}

func createAppIcon(instance uintptr) uintptr {
	const size = 32
	xor := make([]byte, size*size*4)
	and := make([]byte, size*size/8)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			i := (y*size + x) * 4
			dx := x - size/2
			dy := y - size/2
			if dx*dx+dy*dy > 15*15 {
				xor[i+3] = 0
				continue
			}
			xor[i+0] = 153
			xor[i+1] = 211
			xor[i+2] = 52
			xor[i+3] = 255
			if x > 10 && x < 22 && y > 10 && y < 22 {
				xor[i+0] = 42
				xor[i+1] = 39
				xor[i+2] = 15
			}
		}
	}
	icon, _, _ := procCreateIcon.Call(
		instance,
		size,
		size,
		1,
		32,
		uintptr(unsafe.Pointer(&and[0])),
		uintptr(unsafe.Pointer(&xor[0])),
	)
	return icon
}

func utf16Ptr(s string) *uint16 {
	s = strings.ReplaceAll(s, "\n", "\r\n")
	return syscall.StringToUTF16Ptr(s)
}
