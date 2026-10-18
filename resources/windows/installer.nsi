Unicode true
!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "x64.nsh"
!include "FileFunc.nsh"
!include "package.nsh"
!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${BOT_ID}"

Name "${BOT_NAME}"
OutFile "${BOT_OUTPUT}"
InstallDir "$LOCALAPPDATA\Programs\${BOT_NAME}"
InstallDirRegKey HKCU "${UNINSTALL_KEY}" "InstallLocation"
RequestExecutionLevel user
SetCompressor /SOLID zlib
ShowInstDetails show
ShowUninstDetails show
BrandingText "Caelis Bot"
Var StartupOwned
VIProductVersion "${BOT_NUMERIC_VERSION}"
VIAddVersionKey /LANG=1033 "ProductName" "${BOT_NAME}"
VIAddVersionKey /LANG=1033 "FileDescription" "${BOT_NAME} Setup"
VIAddVersionKey /LANG=1033 "FileVersion" "${BOT_VERSION}"
VIAddVersionKey /LANG=1033 "LegalCopyright" "Caelis Bot contributors"
!define MUI_ICON "${BOT_ICON}"
!define MUI_UNICON "${BOT_ICON}"
!define MUI_ABORTWARNING
!define MUI_WELCOMEPAGE_TITLE "${BOT_NAME}"
!define MUI_WELCOMEPAGE_TEXT "$(WelcomeDescription)"
!define MUI_FINISHPAGE_RUN "$INSTDIR\caelis-bot.exe"
!define MUI_FINISHPAGE_RUN_NOTCHECKED
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "${BOT_LICENSE}"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_UNPAGE_FINISH
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"
SetFont /LANG=${LANG_ENGLISH} "Segoe UI" 9
SetFont /LANG=${LANG_SIMPCHINESE} "Microsoft YaHei UI" 9

LangString WelcomeDescription ${LANG_ENGLISH} "Install ${BOT_NAME} on your computer.$\r$\n$\r$\nClick Next to continue."
LangString WelcomeDescription ${LANG_SIMPCHINESE} "即将在你的电脑上安装 ${BOT_NAME}。$\r$\n$\r$\n点击“下一步”继续。"

LangString CloseBot ${LANG_ENGLISH} "Close Caelis Bot using its tray menu, then run Setup again. Your work and data will be preserved."
LangString CloseBot ${LANG_SIMPCHINESE} "请从托盘菜单退出 Caelis Bot，然后重新运行安装程序。工作与数据会保留。"
LangString CloseTerminals ${LANG_ENGLISH} "Close local task terminals, then run Setup again. Their Workers keep running."
LangString CloseTerminals ${LANG_SIMPCHINESE} "请先关闭本地任务终端，再重新运行安装程序。Worker 仍会继续运行。"
LangString WrongOwner ${LANG_ENGLISH} "The installation folder belongs to another installation, is redirected, or has been moved. Setup stopped without removing it."
LangString WrongOwner ${LANG_SIMPCHINESE} "安装目录属于其他安装、已重定向或被移动。安装程序已停止，未删除该目录。"
LangString Unsupported ${LANG_ENGLISH} "Caelis Bot requires Windows 10 22H2 or later (x64)."
LangString Unsupported ${LANG_SIMPCHINESE} "Caelis Bot 需要 Windows 10 22H2 或更高版本（x64）。"
LangString WebViewMissing ${LANG_ENGLISH} "Install Microsoft WebView2 Runtime, then run Setup again."
LangString WebViewMissing ${LANG_SIMPCHINESE} "请先安装 Microsoft WebView2 Runtime，然后重新运行安装程序。"
LangString ExistingLocation ${LANG_ENGLISH} "To update Caelis Bot, choose its existing installation folder. To move it, uninstall it first; your data will be kept."
LangString ExistingLocation ${LANG_SIMPCHINESE} "更新时请选择原安装目录。如需移动，请先卸载；你的数据会保留。"

!macro GuardInstallation PREFIX
Function ${PREFIX}GuardInstallation
  ; Accept a user-selected local folder, never a volume root, UNC path or
  ; redirected ancestor. Check every existing ancestor, on any local drive.
  StrCpy $0 $INSTDIR 2 1
  StrCmp $0 ':\' 0 foreign
  ; NSIS GetFullPathName requires existing parents. The Win32 function also
  ; normalizes a fresh, nested destination before any directory is created.
  System::Call 'kernel32::GetFullPathNameW(w "$INSTDIR", i ${NSIS_MAX_STRLEN}, w .r5, p 0) i.r0'
  IntCmp $0 0 foreign
  IntCmp $0 ${NSIS_MAX_STRLEN} foreign 0 foreign
  StrCpy $INSTDIR $5
  StrCpy $0 $INSTDIR 1 -1
  StrCmp $0 '\' 0 +2
    StrCpy $INSTDIR $INSTDIR -1
  ${GetRoot} "$INSTDIR" $2
  StrCmp $INSTDIR "$2" foreign
  StrCpy $3 $INSTDIR
parents:
  System::Call 'kernel32::GetFileAttributesW(w r3) i.r0'
  StrCmp $0 -1 parent_next
  IntOp $1 $0 & 0x400
  IntCmp $1 0 +2
    Goto foreign
parent_next:
  ${GetParent} "$3" $4
  StrCmp $4 "" parents_done
  StrCmp $4 $3 parents_done
  StrCpy $3 $4
  Goto parents
parents_done:
  ReadRegStr $0 HKCU "${UNINSTALL_KEY}" "InstallLocation"
  StrCmp $0 "" registration_checked
  GetFullPathName $0 "$0"
  StrCmp $0 $INSTDIR registration_checked
  SetErrorLevel 104
  IfSilent +2
    MessageBox MB_OK|MB_ICONEXCLAMATION "$(ExistingLocation)"
  Quit
registration_checked:
  System::Call 'kernel32::GetFileAttributesW(w "$INSTDIR") i.r0'
  StrCmp $0 -1 done
  IntOp $1 $0 & 0x400
  IntCmp $1 0 +2
    Goto foreign
  IfFileExists "$INSTDIR\installation.ini" 0 no_marker
  ReadINIStr $0 "$INSTDIR\installation.ini" "CaelisBot" "Identity"
  StrCmp $0 "${BOT_ID}" 0 foreign
  ReadINIStr $0 "$INSTDIR\installation.ini" "CaelisBot" "Location"
  StrCmp $0 "" legacy
  GetFullPathName $0 "$0"
  StrCmp $0 $INSTDIR done foreign
legacy:
  ; Existing preview installers wrote no Location and only supported this root.
  StrCmp $INSTDIR "$LOCALAPPDATA\Programs\${BOT_NAME}" done foreign
no_marker:
  ; A pre-existing nonempty directory is never adopted as our installation.
  FindFirst $0 $1 "$INSTDIR\*"
scan:
  StrCmp $1 "" empty
  StrCmp $1 "." next
  StrCmp $1 ".." next foreign_scan
next:
  FindNext $0 $1
  Goto scan
foreign_scan:
  FindClose $0
  Goto foreign
empty:
  FindClose $0
  Goto done
foreign:
  SetErrorLevel 104
  IfSilent +2
    MessageBox MB_OK|MB_ICONSTOP "$(WrongOwner)"
  Quit
done:
  !insertmacro GuardPayloadDirectories
FunctionEnd

Function ${PREFIX}EnsureStopped
  IfFileExists "$INSTDIR\caelis-bot.exe" 0 terminal
  ; Opening the mapped executable for writing fails while any Bot process uses
  ; it, including its MCP tool child. Never terminate work to install an update.
  System::Call 'kernel32::CreateFileW(w "$INSTDIR\caelis-bot.exe", i 0x40000000, i 7, p 0, i 3, i 0, p 0) p.r0'
  IntCmp $0 -1 busy
  System::Call 'kernel32::CloseHandle(p r0)'
  Goto terminal
busy:
  SetErrorLevel 103
  IfSilent +2
    MessageBox MB_OK|MB_ICONEXCLAMATION "$(CloseBot)"
  Quit
terminal:
  IfFileExists "$INSTDIR\caelis-terminal.exe" 0 done
  ; End only our pure console launcher. Its independent runtime clients and
  ; Workers are not process-tree children owned by Setup.
  ExecWait '"$INSTDIR\caelis-bot.exe" --close-terminal-helpers-for-update' $0
  System::Call 'kernel32::CreateFileW(w "$INSTDIR\caelis-terminal.exe", i 0x40000000, i 7, p 0, i 3, i 0, p 0) p.r0'
  IntCmp $0 -1 terminal_busy
  System::Call 'kernel32::CloseHandle(p r0)'
  Goto done
terminal_busy:
  SetErrorLevel 103
  IfSilent +2
    MessageBox MB_OK|MB_ICONEXCLAMATION "$(CloseTerminals)"
  Quit
done:
FunctionEnd
!macroend
!insertmacro GuardInstallation ""
!insertmacro GuardInstallation "un."

Function .onInit
  SetShellVarContext current
  SetRegView 64
  ${IfNot} ${RunningX64}
    Goto unsupported
  ${EndIf}
  ; GetNativeSystemInfo also excludes ARM64 running x64 emulation.
  System::Call '*(&i2 0, &i2 0, i 0, p 0, p 0, p 0, i 0, i 0, i 0, &i2 0, &i2 0) p.r0'
  System::Call 'kernel32::GetNativeSystemInfo(p r0)'
  System::Call '*$0(&i2 .r1)'
  System::Free $0
  IntCmp $1 9 +2
    Goto unsupported
  System::Call '*(&i4 276, &i4 0, &i4 0, &i4 0, &i4 0, &w128 "") p.r0'
  System::Call 'ntdll::RtlGetVersion(p r0) i.r2'
  System::Call '*$0(i, i .r1, i, i .r3)'
  System::Free $0
  IntCmp $2 0 +2
    Goto unsupported
  IntCmp $1 10 0 unsupported
  IntCmp $3 19045 0 unsupported
  ; Microsoft's documented WebView2 Evergreen registry discovery contract.
  !define WEBVIEW_CLIENT "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"
  ReadRegStr $0 HKCU "${WEBVIEW_CLIENT}" "pv"
  StrCmp $0 "" user32
  StrCmp $0 "0.0.0.0" user32 ready
user32:
  SetRegView 32
  ReadRegStr $0 HKCU "${WEBVIEW_CLIENT}" "pv"
  SetRegView 64
  StrCmp $0 "" machine
  StrCmp $0 "0.0.0.0" machine ready
machine:
  SetRegView 32
  ReadRegStr $0 HKLM "${WEBVIEW_CLIENT}" "pv"
  SetRegView 64
  StrCmp $0 "" machine64
  StrCmp $0 "0.0.0.0" machine64 ready
machine64:
  ReadRegStr $0 HKLM "${WEBVIEW_CLIENT}" "pv"
  StrCmp $0 "" webview_missing
  StrCmp $0 "0.0.0.0" webview_missing ready
unsupported:
  SetErrorLevel 105
  IfSilent +2
    MessageBox MB_OK|MB_ICONSTOP "$(Unsupported)"
  Quit
webview_missing:
  SetErrorLevel 106
  IfSilent +2
    MessageBox MB_OK|MB_ICONSTOP "$(WebViewMissing)"
  Quit
ready:
FunctionEnd

Section "Install"
  ; The app or directory may have changed while the welcome/license pages were
  ; open. Recheck ownership and running images immediately before mutation.
  Call GuardInstallation
  Call EnsureStopped
  StrCpy $StartupOwned 0
  ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${BOT_ID}"
  StrCmp $0 '$\"$INSTDIR\caelis-bot.exe$\" --login-launch' 0 +2
    StrCpy $StartupOwned 1
  ; Use the old inventory to remove obsolete program files during upgrades.
  IfFileExists "$INSTDIR\Uninstall.exe" 0 install
  ExecWait '"$INSTDIR\Uninstall.exe" /S _?=$INSTDIR' $0
  StrCmp $0 0 install
  SetErrorLevel $0
  Abort
install:
  ClearErrors
  !insertmacro InstallPayload
  SetOutPath "$INSTDIR\Resources\Windows"
  File /oname=NSIS-LICENSE.txt "${BOT_NSIS_LICENSE}"
  IfErrors installation_failed
  ; Preserve Unicode paths independently of the machine's ANSI code page.
  FileOpen $0 "$INSTDIR\installation.ini" w
  FileWriteUTF16LE /BOM $0 "[CaelisBot]$\r$\nIdentity=${BOT_ID}$\r$\nVersion=${BOT_VERSION}$\r$\nLocation=$INSTDIR$\r$\n"
  FileClose $0
  IfErrors installation_failed
  StrCmp $StartupOwned 1 0 +2
    WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${BOT_ID}" '$\"$INSTDIR\caelis-bot.exe$\" --login-launch'
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateDirectory "$SMPROGRAMS\${BOT_NAME}"
  CreateShortCut "$SMPROGRAMS\${BOT_NAME}\${BOT_NAME}.lnk" "$INSTDIR\caelis-bot.exe" "" "$INSTDIR\caelis-bot.exe" 0
  CreateShortCut "$SMPROGRAMS\${BOT_NAME}\Uninstall.lnk" "$INSTDIR\Uninstall.exe"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "${BOT_NAME}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${BOT_VERSION}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "Publisher" "Caelis Bot contributors"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" "$INSTDIR\caelis-bot.exe"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '$\"$INSTDIR\Uninstall.exe$\"'
  WriteRegStr HKCU "${UNINSTALL_KEY}" "QuietUninstallString" '$\"$INSTDIR\Uninstall.exe$\" /S'
  WriteRegStr HKCU "${UNINSTALL_KEY}" "URLInfoAbout" "https://github.com/caelis-labs/caelis-bot"
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1
  Goto done
installation_failed:
  SetErrorLevel 107
  Abort
done:
SectionEnd

Function un.onInit
  SetShellVarContext current
  SetRegView 64
  Call un.GuardInstallation
  Call un.EnsureStopped
FunctionEnd

Section "Uninstall"
  ; Remove only notification identities still owned by this installation.
  ExecWait '"$INSTDIR\caelis-bot.exe" --unregister-notifications' $0
  ; Remove only this exact installation's public Run value. An independent ZIP
  ; or another channel may own a different entry; never change its preference.
  ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${BOT_ID}"
  StrCmp $0 '$\"$INSTDIR\caelis-bot.exe$\" --login-launch' 0 +2
    DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${BOT_ID}"
  Delete "$INSTDIR\Resources\Windows\NSIS-LICENSE.txt"
  !insertmacro UninstallPayload
  Delete "$INSTDIR\installation.ini"
  Delete "$INSTDIR\Uninstall.exe"
  ; No recursive removal: unknown files and all data/credentials survive.
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\${BOT_NAME}\${BOT_NAME}.lnk"
  Delete "$SMPROGRAMS\${BOT_NAME}\Uninstall.lnk"
  RMDir "$SMPROGRAMS\${BOT_NAME}"
  DeleteRegKey HKCU "${UNINSTALL_KEY}"
SectionEnd
