; FurkVPN installer for Windows 10/11 x64.
;   makensis -DVERSION=1.0.0 -DVERSIONCODE=1 -DDIST=..\dist -DOUT=..\dist\FurkVPN-Setup-1.0.0.exe furkvpn.nsi
; Installs the app and its service (FurkVPNService.exe, LocalSystem), registers furkvpn:// links
; and settings files, makes sure WebView2 is there. "/S /UPDATE" (from the app's updater) installs
; silently and starts the app again.

Unicode true
ManifestDPIAware true
RequestExecutionLevel admin
SetCompressor /SOLID lzma
ShowInstDetails nevershow
ShowUninstDetails nevershow

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!ifndef VERSIONCODE
  !define VERSIONCODE "0"
!endif
!ifndef DIST
  !define DIST "..\dist"
!endif
!ifndef OUT
  !define OUT "${DIST}\FurkVPN-Setup-${VERSION}.exe"
!endif

!define APP "FurkVPN"
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\FurkVPN"
!define WEBVIEW2_KEY "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"

Name "${APP}"
OutFile "${OUT}"
InstallDir "$PROGRAMFILES64\${APP}"
BrandingText "${APP} ${VERSION}"

VIProductVersion "${VERSION}.0"
VIAddVersionKey /LANG=1049 "ProductName" "${APP}"
VIAddVersionKey /LANG=1049 "FileDescription" "Установка ${APP}"
VIAddVersionKey /LANG=1049 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=1049 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=1049 "CompanyName" "${APP}"
VIAddVersionKey /LANG=1049 "LegalCopyright" "© ${APP}"

!include "MUI2.nsh"
!include "FileFunc.nsh"
!include "LogicLib.nsh"
!include "x64.nsh"
!include "WinVer.nsh"

!define MUI_ICON "..\build\appicon.ico"
!define MUI_UNICON "..\build\appicon.ico"
!define MUI_WELCOMEFINISHPAGE_BITMAP "..\build\installer-side.bmp"
!define MUI_UNWELCOMEFINISHPAGE_BITMAP "..\build\installer-side.bmp"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_FUNCTION StartApp

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "Russian"
!insertmacro MUI_LANGUAGE "English"

LangString NeedsWin10 1049 "FurkVPN работает на Windows 10 и 11 (64-бит)."
LangString NeedsWin10 1033 "FurkVPN needs 64-bit Windows 10 or 11."
LangString WebView2Failed 1049 "Не удалось установить Microsoft Edge WebView2, без него окно FurkVPN не откроется. Проверьте интернет и запустите установку снова."
LangString WebView2Failed 1033 "Couldn't install Microsoft Edge WebView2; the FurkVPN window won't open without it. Check your internet connection and run the installer again."
LangString ServiceFailed 1049 "Не удалось зарегистрировать службу FurkVPN."
LangString ServiceFailed 1033 "Couldn't register the FurkVPN service."
LangString LinkProtocol 1049 "Ссылка FurkVPN"
LangString LinkProtocol 1033 "FurkVPN link"
LangString SettingsFile 1049 "Файл настроек FurkVPN"
LangString SettingsFile 1033 "FurkVPN settings file"

Var Update

Function .onInit
  ${IfNot} ${RunningX64}
  ${OrIfNot} ${AtLeastWin10}
    MessageBox MB_ICONSTOP $(NeedsWin10) /SD IDOK
    Abort
  ${EndIf}
  SetRegView 64
  ${GetParameters} $0
  ClearErrors
  ${GetOptions} $0 "/UPDATE" $1
  ${IfNot} ${Errors}
    StrCpy $Update "1"
  ${EndIf}
  ; Installed before: the same folder.
  ReadRegStr $1 HKLM "${UNINSTKEY}" "InstallLocation"
  ${If} $1 != ""
    StrCpy $INSTDIR $1
  ${EndIf}
FunctionEnd

Function un.onInit
  SetRegView 64
FunctionEnd

; The app's window may be open in any user's session; the installer replaces its file.
!macro StopApp
  nsExec::Exec '"$SYSDIR\taskkill.exe" /F /IM FurkVPN.exe /T'
  Pop $0
!macroend

Function StartApp
  ; Through Explorer, so the app runs as the user and not as administrator.
  Exec '"$WINDIR\explorer.exe" "$INSTDIR\FurkVPN.exe"'
FunctionEnd

Section "Install"
  SetShellVarContext all
  !insertmacro StopApp
  ; The old service holds its files; it is stopped, not removed, so the VPN's settings stay.
  ${If} ${FileExists} "$INSTDIR\FurkVPNService.exe"
    nsExec::Exec '"$SYSDIR\sc.exe" stop FurkVPN'
    Pop $0
    Sleep 1500
  ${EndIf}

  SetOutPath "$INSTDIR"
  SetOverwrite on
  ClearErrors
  File "${DIST}\FurkVPN.exe"
  File "${DIST}\FurkVPNService.exe"
  File "${DIST}\wintun.dll"
  File "${DIST}\LICENSE-wintun.txt"
  File "${DIST}\THIRD_PARTY.txt"
  ${If} ${Errors}
    ; A file was still in use: one more try after the service has surely stopped.
    Sleep 3000
    File "${DIST}\FurkVPN.exe"
    File "${DIST}\FurkVPNService.exe"
    File "${DIST}\wintun.dll"
  ${EndIf}
  ; The stopped app couldn't put back the system proxy it may have set (the VPN without TUN):
  ; the new one does, and sets it again itself once the VPN is back on.
  nsExec::Exec '"$INSTDIR\FurkVPN.exe" --restore-proxy'
  Pop $0

  ; WebView2 draws the window; Windows 11 has it, older Windows 10 may not.
  ReadRegStr $0 HKLM "${WEBVIEW2_KEY}" "pv"
  ReadRegStr $1 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  ${If} $0 == ""
  ${OrIf} $0 == "0.0.0.0"
    ${If} $1 == ""
    ${OrIf} $1 == "0.0.0.0"
      SetOutPath "$PLUGINSDIR"
      File "${DIST}\MicrosoftEdgeWebview2Setup.exe"
      ExecWait '"$PLUGINSDIR\MicrosoftEdgeWebview2Setup.exe" /silent /install' $2
      ${If} $2 != 0
        MessageBox MB_ICONEXCLAMATION $(WebView2Failed) /SD IDOK
      ${EndIf}
      SetOutPath "$INSTDIR"
    ${EndIf}
  ${EndIf}

  ; The service: registered (or pointed at the new file), startable by signed-in users from the
  ; app, then started.
  nsExec::ExecToLog '"$INSTDIR\FurkVPNService.exe" install'
  Pop $0
  ${If} $0 != 0
    MessageBox MB_ICONSTOP $(ServiceFailed) /SD IDOK
  ${EndIf}
  nsExec::Exec '"$SYSDIR\sc.exe" sdset FurkVPN "D:(A;;CCLCSWRPWPDTLOCRRC;;;SY)(A;;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;BA)(A;;CCLCSWLOCRRC;;;IU)(A;;CCLCSWLOCRRC;;;SU)(A;;RP;;;IU)S:(AU;FA;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;WD)"'
  Pop $0
  nsExec::Exec '"$SYSDIR\sc.exe" start FurkVPN'
  Pop $0

  ; furkvpn:// links from the website and the bot.
  WriteRegStr HKLM "Software\Classes\furkvpn" "" "$(LinkProtocol)"
  WriteRegStr HKLM "Software\Classes\furkvpn" "URL Protocol" ""
  WriteRegStr HKLM "Software\Classes\furkvpn\DefaultIcon" "" '"$INSTDIR\FurkVPN.exe",0'
  WriteRegStr HKLM "Software\Classes\furkvpn\shell\open\command" "" '"$INSTDIR\FurkVPN.exe" "%1"'
  ; "Open with FurkVPN" for settings files (.json), without taking the extension over.
  WriteRegStr HKLM "Software\Classes\FurkVPN.SettingsFile" "" "$(SettingsFile)"
  WriteRegStr HKLM "Software\Classes\FurkVPN.SettingsFile\DefaultIcon" "" '"$INSTDIR\FurkVPN.exe",0'
  WriteRegStr HKLM "Software\Classes\FurkVPN.SettingsFile\shell\open\command" "" '"$INSTDIR\FurkVPN.exe" "%1"'
  WriteRegStr HKLM "Software\Classes\.json\OpenWithProgids" "FurkVPN.SettingsFile" ""
  WriteRegStr HKLM "Software\Classes\Applications\FurkVPN.exe\shell\open\command" "" '"$INSTDIR\FurkVPN.exe" "%1"'

  CreateShortcut "$SMPROGRAMS\${APP}.lnk" "$INSTDIR\FurkVPN.exe"
  ${If} $Update != "1"
    CreateShortcut "$DESKTOP\${APP}.lnk" "$INSTDIR\FurkVPN.exe"
  ${EndIf}

  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayName" "${APP}"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegDWORD HKLM "${UNINSTKEY}" "VersionCode" ${VERSIONCODE}
  WriteRegStr HKLM "${UNINSTKEY}" "Publisher" "${APP}"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayIcon" '"$INSTDIR\FurkVPN.exe",0'
  WriteRegStr HKLM "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNINSTKEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKLM "${UNINSTKEY}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegStr HKLM "${UNINSTKEY}" "URLInfoAbout" "https://furkapp.net"
  WriteRegDWORD HKLM "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINSTKEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKLM "${UNINSTKEY}" "EstimatedSize" "$0"

  ${If} $Update == "1"
    Call StartApp
  ${EndIf}
SectionEnd

Section "Uninstall"
  SetShellVarContext all
  !insertmacro StopApp
  nsExec::Exec '"$INSTDIR\FurkVPN.exe" --restore-proxy'
  Pop $0
  nsExec::ExecToLog '"$INSTDIR\FurkVPNService.exe" uninstall'
  Pop $0
  nsExec::Exec '"$SYSDIR\netsh.exe" advfirewall firewall delete rule "name=FurkVPN: раздача интернета"'
  Pop $0
  Sleep 1000

  Delete "$INSTDIR\FurkVPN.exe"
  Delete "$INSTDIR\FurkVPNService.exe"
  Delete "$INSTDIR\wintun.dll"
  Delete "$INSTDIR\LICENSE-wintun.txt"
  Delete "$INSTDIR\THIRD_PARTY.txt"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\${APP}.lnk"
  Delete "$DESKTOP\${APP}.lnk"
  DeleteRegKey HKLM "Software\Classes\furkvpn"
  DeleteRegKey HKLM "Software\Classes\FurkVPN.SettingsFile"
  DeleteRegValue HKLM "Software\Classes\.json\OpenWithProgids" "FurkVPN.SettingsFile"
  DeleteRegKey HKLM "Software\Classes\Applications\FurkVPN.exe"
  DeleteRegKey HKLM "${UNINSTKEY}"
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "FurkVPN"

  ; The key and the settings go with the app, like on Android.
  ReadEnvStr $0 ProgramData
  RMDir /r "$0\FurkVPN"
  RMDir /r "$0\FurkVPN Updates"
  SetShellVarContext current
  RMDir /r "$APPDATA\FurkVPN"
  RMDir /r "$LOCALAPPDATA\FurkVPN"
SectionEnd
