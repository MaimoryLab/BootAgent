Unicode true
!include "x64.nsh"
!include "WinVer.nsh"
!include "FileFunc.nsh"
!include "MUI2.nsh"

!ifndef INFO_PROJECTNAME
  !define INFO_PROJECTNAME "bootagent-desktop"
!endif
!ifndef INFO_COMPANYNAME
  !define INFO_COMPANYNAME "MaimoryLab"
!endif
!ifndef INFO_PRODUCTNAME
  !define INFO_PRODUCTNAME "BootAgent"
!endif
!ifndef INFO_PRODUCTVERSION
  !define INFO_PRODUCTVERSION "0.3.0"
!endif
!ifndef INFO_COPYRIGHT
  !define INFO_COPYRIGHT "(c) 2026 MaimoryLab"
!endif
!ifndef PRODUCT_EXECUTABLE
  !define PRODUCT_EXECUTABLE "${INFO_PROJECTNAME}.exe"
!endif
!ifndef WAILS_INSTALL_SCOPE
  !define WAILS_INSTALL_SCOPE "machine"
!endif
!ifndef REQUEST_EXECUTION_LEVEL
  !if "${WAILS_INSTALL_SCOPE}" == "user"
    !define REQUEST_EXECUTION_LEVEL "user"
  !else
    !define REQUEST_EXECUTION_LEVEL "admin"
  !endif
!endif
!ifdef ARG_WAILS_AMD64_BINARY
  !define SUPPORTS_AMD64
!endif
!ifdef ARG_WAILS_ARM64_BINARY
  !define SUPPORTS_ARM64
!endif
!ifdef SUPPORTS_AMD64
  !define ARCH "amd64"
!else
  !ifdef SUPPORTS_ARM64
    !define ARCH "arm64"
  !else
    !error "Provide ARG_WAILS_AMD64_BINARY or ARG_WAILS_ARM64_BINARY"
  !endif
!endif

RequestExecutionLevel "${REQUEST_EXECUTION_LEVEL}"
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion "${INFO_PRODUCTVERSION}.0"
VIAddVersionKey "CompanyName" "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion" "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright" "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName" "${INFO_PRODUCTNAME}"
ManifestDPIAware true
!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
!if "${WAILS_INSTALL_SCOPE}" == "user"
  InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
!else
  InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
!endif
ShowInstDetails show

Function .onInit
  ${IfNot} ${AtLeastWin10}
    MessageBox MB_OK "BootAgent requires Windows 10 or later."
    Quit
  ${EndIf}
  !ifdef SUPPORTS_AMD64
    ${If} ${IsNativeAMD64}
      Return
    ${EndIf}
  !endif
  !ifdef SUPPORTS_ARM64
    ${If} ${IsNativeARM64}
      Return
    ${EndIf}
  !endif
  MessageBox MB_OK "This installer does not support the current Windows architecture."
  Quit
FunctionEnd

Section
  !if "${REQUEST_EXECUTION_LEVEL}" == "admin"
    SetShellVarContext all
  !else
    SetShellVarContext current
  !endif
  SetRegView 64
  ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
  !if "${WAILS_INSTALL_SCOPE}" == "user"
    ${If} $0 == ""
      ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
    ${EndIf}
  !endif
  ${If} $0 == ""
    InitPluginsDir
    SetOutPath "$PLUGINSDIR"
    File "MicrosoftEdgeWebview2Setup.exe"
    DetailPrint "Installing: WebView2 Runtime"
    ExecWait '"$PLUGINSDIR\MicrosoftEdgeWebview2Setup.exe" /silent /install'
  ${EndIf}
  SetOutPath "$INSTDIR"
  !ifdef SUPPORTS_AMD64
    !if "${ARCH}" == "amd64"
      File /oname=${PRODUCT_EXECUTABLE} "${ARG_WAILS_AMD64_BINARY}"
    !endif
  !endif
  !ifdef SUPPORTS_ARM64
    !if "${ARCH}" == "arm64"
      File /oname=${PRODUCT_EXECUTABLE} "${ARG_WAILS_ARM64_BINARY}"
    !endif
  !endif
  CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
  CreateShortcut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
  WriteUninstaller "$INSTDIR\uninstall.exe"
  !if "${WAILS_INSTALL_SCOPE}" == "user"
    WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BootAgent" "DisplayName" "${INFO_PRODUCTNAME}"
    WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BootAgent" "DisplayVersion" "${INFO_PRODUCTVERSION}"
    WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BootAgent" "UninstallString" '"$INSTDIR\uninstall.exe"'
  !else
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\BootAgent" "DisplayName" "${INFO_PRODUCTNAME}"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\BootAgent" "DisplayVersion" "${INFO_PRODUCTVERSION}"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\BootAgent" "UninstallString" '"$INSTDIR\uninstall.exe"'
  !endif
SectionEnd

Section "uninstall"
  !if "${WAILS_INSTALL_SCOPE}" == "user"
    SetShellVarContext current
  !else
    SetShellVarContext all
  !endif
  Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
  Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"
  RMDir /r "$APPDATA\${PRODUCT_EXECUTABLE}"
  Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  !if "${WAILS_INSTALL_SCOPE}" == "user"
    DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\BootAgent"
  !else
    DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\BootAgent"
  !endif
SectionEnd
