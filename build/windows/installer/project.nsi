Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them with the values from ProjectInfo.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
##
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the ProjectInfo file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "MyProject" # Default "{{.Name}}"
## !define INFO_COMPANYNAME    "MyCompany" # Default "{{.Info.CompanyName}}"
## !define INFO_PRODUCTNAME    "MyProduct" # Default "{{.Info.ProductName}}"
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "{{.Info.ProductVersion}}"
## !define INFO_COPYRIGHT      "Copyright" # Default "{{.Info.Copyright}}"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
## !define REQUEST_EXECUTION_LEVEL "admin"            # Default "admin"  see also https://nsis.sourceforge.io/Docs/Chapter4.html
####

## Include the wails tools
!include "wails_tools.nsh"
!include "LogicLib.nsh"

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp"
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

!insertmacro MUI_PAGE_WELCOME
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

## Signing hooks (optional)
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

# ── Registry key for model config ────────────────────────────────────────────
!define NETMONIT_REG_KEY   "Software\NetMonit"
!define NETMONIT_REG_VALUE "SelectedModel"

# ── AI dependency constants ───────────────────────────────────────────────────

!define OLLAMA_EXE_PATH  "$LOCALAPPDATA\Programs\Ollama\ollama.exe"
!define OLLAMA_REG_KEY   "SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Ollama"
!define OLLAMA_URL       "https://github.com/ollama/ollama/releases/latest/download/OllamaSetup.exe"

!define ONNX_MODEL_URL   "https://github.com/hadinurhakim-coding/net-monit/releases/latest/download/netmonit-classifier.onnx"
!define ONNX_TOKEN_URL   "https://github.com/hadinurhakim-coding/net-monit/releases/latest/download/spiece.model"
!define ONNX_MODELS_DIR  "$APPDATA\net-monit\models"

# ── Macro: Detect GPU VRAM & select optimal LLM model ────────────────────────
#
# Uses PowerShell + WMI to query AdapterRAM from Win32_VideoController.
# Decision:
#   VRAM >= 10 GB  → deepseek-r1:14b
#   VRAM >= 6 GB   → deepseek-r1:7b
#   VRAM >= 3 GB   → deepseek-r1:1.5b  (dedicated GPU tapi kecil)
#   No GPU / iGPU  → deepseek-r1:1.5b  (CPU mode)
#
# Result is written to HKCU\Software\NetMonit\SelectedModel so the Go app
# can read it at runtime via the Windows registry.

!macro DetectGPUAndSelectModel
    DetailPrint "Detecting GPU and selecting optimal AI model..."

    ; Run PowerShell to get max dedicated VRAM in MB across all video controllers
    ; AdapterRAM is in bytes; divide by 1MB. Filter out Microsoft Basic Display (iGPU fallback).
    nsExec::ExecToStack 'powershell.exe -NoProfile -NonInteractive -Command "\
        $vram = (Get-WmiObject Win32_VideoController | \
            Where-Object { $_.AdapterRAM -gt 0 -and $_.Name -notmatch ''Basic Display'' } | \
            Measure-Object -Property AdapterRAM -Maximum).Maximum; \
        if ($vram) { [math]::Round($vram / 1MB) } else { 0 }"'
    Pop $0   ; exit code
    Pop $1   ; stdout = VRAM in MB (or 0)

    ; Trim whitespace/newlines from output
    StrCpy $1 $1 -2   ; remove trailing \r\n

    DetailPrint "Detected GPU VRAM: $1 MB"

    ; Default fallback
    StrCpy $2 "deepseek-r1:1.5b"

    ; $1 is a string — compare numerically via IntCmp
    IntCmp $1 10240 ModelIs14b ModelCheckNext ModelIs14b
    ModelCheckNext:
    IntCmp $1 6144 ModelIs7b ModelCheckSmall ModelIs7b
    ModelCheckSmall:
    IntCmp $1 3072 ModelIs1b ModelIs1b ModelIs1b
    Goto WriteModel

    ModelIs14b:
        StrCpy $2 "deepseek-r1:14b"
        DetailPrint "Selected model: deepseek-r1:14b (VRAM >= 10 GB)"
        Goto WriteModel

    ModelIs7b:
        StrCpy $2 "deepseek-r1:7b"
        DetailPrint "Selected model: deepseek-r1:7b (VRAM >= 6 GB)"
        Goto WriteModel

    ModelIs1b:
        StrCpy $2 "deepseek-r1:1.5b"
        DetailPrint "Selected model: deepseek-r1:1.5b (VRAM < 6 GB or CPU only)"

    WriteModel:
        ; Write chosen model to HKCU registry so Go app can read it
        WriteRegStr HKCU "${NETMONIT_REG_KEY}" "${NETMONIT_REG_VALUE}" "$2"
        DetailPrint "Model written to registry: $2"
!macroend

# ── Macro: Check & silently install Ollama ────────────────────────────────────
#
# Detection order:
#   1. HKLM uninstall registry key (standard install)
#   2. HKCU uninstall registry key (per-user install)
#   3. Well-known exe path in %LOCALAPPDATA%

!macro CheckAndInstallOllama
    DetailPrint "Checking for Ollama..."

    ReadRegStr $0 HKLM "${OLLAMA_REG_KEY}" "InstallLocation"
    ${If} $0 != ""
        DetailPrint "Ollama already installed (HKLM registry)."
        Goto OllamaDone
    ${EndIf}

    ReadRegStr $0 HKCU "${OLLAMA_REG_KEY}" "InstallLocation"
    ${If} $0 != ""
        DetailPrint "Ollama already installed (HKCU registry)."
        Goto OllamaDone
    ${EndIf}

    IfFileExists "${OLLAMA_EXE_PATH}" OllamaFound OllamaNotFound

    OllamaFound:
        DetailPrint "Ollama already installed (exe found)."
        Goto OllamaDone

    OllamaNotFound:
        DetailPrint "Ollama not found. Downloading installer..."
        NSISdl::download /TIMEOUT=120000 "${OLLAMA_URL}" "$TEMP\OllamaSetup.exe"
        Pop $0
        ${If} $0 == "success"
            DetailPrint "Running Ollama installer silently..."
            ExecWait '"$TEMP\OllamaSetup.exe" /S' $0
            ${If} $0 != 0
                DetailPrint "Ollama installer exited with code $0 (may still be OK)."
            ${Else}
                DetailPrint "Ollama installed successfully."
            ${EndIf}
            Delete "$TEMP\OllamaSetup.exe"
        ${Else}
            DetailPrint "Ollama download failed: $0. AI chat requires Ollama to be installed manually."
        ${EndIf}

    OllamaDone:
!macroend

# ── Macro: Download ONNX model files ─────────────────────────────────────────
#
# Downloads netmonit-classifier.onnx and spiece.model into
# %APPDATA%\net-monit\models\ (matches Go's os.UserConfigDir() path).
# Skips each file if it already exists (idempotent re-runs).

!macro DownloadONNXModel
    DetailPrint "Setting up AI classification model..."
    CreateDirectory "${ONNX_MODELS_DIR}"

    IfFileExists "${ONNX_MODELS_DIR}\netmonit-classifier.onnx" ModelExists ModelMissing

    ModelMissing:
        DetailPrint "Downloading ONNX classifier model (~180 MB)..."
        NSISdl::download /TIMEOUT=300000 "${ONNX_MODEL_URL}" "${ONNX_MODELS_DIR}\netmonit-classifier.onnx"
        Pop $0
        ${If} $0 != "success"
            DetailPrint "ONNX model download failed: $0. Classification will use rule-based fallback."
        ${Else}
            DetailPrint "ONNX model downloaded."
        ${EndIf}
        Goto TokenizerCheck

    ModelExists:
        DetailPrint "ONNX model already present, skipping."

    TokenizerCheck:
    IfFileExists "${ONNX_MODELS_DIR}\spiece.model" TokenizerExists TokenizerMissing

    TokenizerMissing:
        DetailPrint "Downloading tokenizer model (~760 KB)..."
        NSISdl::download /TIMEOUT=60000 "${ONNX_TOKEN_URL}" "${ONNX_MODELS_DIR}\spiece.model"
        Pop $0
        ${If} $0 != "success"
            DetailPrint "Tokenizer download failed: $0."
        ${EndIf}
        Goto ModelsDone

    TokenizerExists:
        DetailPrint "Tokenizer already present, skipping."

    ModelsDone:
!macroend

# ── Macro: Queue model pull based on GPU detection result ────────────────────
#
# Reads the model name written by DetectGPUAndSelectModel from the registry,
# then starts `ollama pull <model>` as a non-blocking background process.

!macro PullDeepSeekModel
    ; Read the selected model back from registry
    ReadRegStr $3 HKCU "${NETMONIT_REG_KEY}" "${NETMONIT_REG_VALUE}"
    ${If} $3 == ""
        StrCpy $3 "deepseek-r1:1.5b"
    ${EndIf}

    DetailPrint "Queuing $3 model download (runs in background)..."
    IfFileExists "${OLLAMA_EXE_PATH}" 0 SkipPull
        Exec '"${OLLAMA_EXE_PATH}" pull $3'
        DetailPrint "$3 pull started in background."
    SkipPull:
!macroend

# ── Installer metadata ────────────────────────────────────────────────────────

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
ShowInstDetails show

Function .onInit
   !insertmacro wails.checkArchitecture
FunctionEnd

# ── Install section ───────────────────────────────────────────────────────────

Section
    !insertmacro wails.setShellContext
    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files

    # Copy ONNX Runtime DLL (placed in build/bin/ by wails build)
    File /nonfatal "..\..\bin\onnxruntime.dll"

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    # AI dependencies (seamless — no user interaction required)
    !insertmacro DetectGPUAndSelectModel
    !insertmacro CheckAndInstallOllama
    !insertmacro DownloadONNXModel
    !insertmacro PullDeepSeekModel

    !insertmacro wails.writeUninstaller
SectionEnd

# ── Uninstall section ─────────────────────────────────────────────────────────
# Note: %APPDATA%\net-monit\ (user data + models) is intentionally preserved.

Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    DeleteRegKey HKCU "${NETMONIT_REG_KEY}"

    !insertmacro wails.deleteUninstaller
SectionEnd
