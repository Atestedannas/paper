// Package docconvert converts legacy Word .doc to .docx without loading the document into memory.
//
// Order of attempts:
//  1) LibreOffice soffice (headless), same as Linux servers — set SOFFICE_PATH if needed.
//  2) Windows only: Microsoft Word COM via cscript + VBScript (requires Word installed; WPS is not Word.Application).
//
// Set DISABLE_WORD_DOC_CONVERT=1 to skip the Word fallback on Windows.
package docconvert

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ConvertDocToDocx converts docPath (.doc) to a .docx in the same directory.
// If deleteSource is true, the original .doc file is removed after success.
func ConvertDocToDocx(docPath string, deleteSource bool) (string, error) {
	absPath, err := filepath.Abs(docPath)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if strings.ToLower(filepath.Ext(absPath)) != ".doc" {
		return "", fmt.Errorf("expected .doc extension, got %s", filepath.Ext(absPath))
	}

	outDir := filepath.Dir(absPath)
	base := strings.TrimSuffix(filepath.Base(absPath), ".doc")
	docxPath := filepath.Join(outDir, base+".docx")

	var sofficeErr error

	// 1) LibreOffice
	if soffice, errSO := resolveSofficeBinary(); errSO == nil {
		cmd := exec.Command(soffice, "--headless", "--convert-to", "docx", "--outdir", outDir, absPath)
		if runtime.GOOS != "windows" {
			cmd.Env = append(os.Environ(), "HOME=/tmp")
		} else {
			cmd.Env = os.Environ()
		}
		out, errRun := cmd.CombinedOutput()
		if errRun == nil {
			if _, errSt := os.Stat(docxPath); errSt == nil {
				if deleteSource {
					_ = os.Remove(absPath)
				}
				return docxPath, nil
			}
			sofficeErr = fmt.Errorf("soffice ran but output missing: %s", string(out))
		} else {
			sofficeErr = fmt.Errorf("soffice: %w, output: %s", errRun, string(out))
		}
		if runtime.GOOS != "windows" {
			return "", sofficeErr
		}
		// Windows: try Word below
	} else {
		sofficeErr = errSO
	}

	wordDisabled := strings.TrimSpace(os.Getenv("DISABLE_WORD_DOC_CONVERT")) != ""

	// 2) Windows: Microsoft Word COM (optional)
	if runtime.GOOS == "windows" && !wordDisabled {
		_ = os.Remove(docxPath)
		errW := convertDocToDocxWordCOM(absPath, docxPath)
		if errW == nil {
			if _, errSt := os.Stat(docxPath); errSt == nil {
				if deleteSource {
					_ = os.Remove(absPath)
				}
				return docxPath, nil
			}
			return "", fmt.Errorf("word com: output not created at %s", docxPath)
		}
		return "", fmt.Errorf("LibreOffice failed (%v); Word COM failed: %w — install LibreOffice (or set SOFFICE_PATH), or install Microsoft Word", sofficeErr, errW)
	}

	if wordDisabled && runtime.GOOS == "windows" {
		return "", fmt.Errorf("LibreOffice: %w (Word fallback disabled via DISABLE_WORD_DOC_CONVERT)", sofficeErr)
	}
	return "", fmt.Errorf("%w — install LibreOffice and ensure soffice is in PATH, or set SOFFICE_PATH (e.g. C:\\Program Files\\LibreOffice\\program\\soffice.exe)", sofficeErr)
}

func resolveSofficeBinary() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("SOFFICE_PATH")); custom != "" {
		if _, err := os.Stat(custom); err == nil {
			return custom, nil
		}
		return "", fmt.Errorf("SOFFICE_PATH set but file missing: %s", custom)
	}
	for _, candidate := range []string{"soffice", "soffice.exe"} {
		if p, err := exec.LookPath(candidate); err == nil {
			return p, nil
		}
	}
	for _, p := range []string{
		`C:\Program Files\LibreOffice\program\soffice.exe`,
		`C:\Program Files (x86)\LibreOffice\program\soffice.exe`,
	} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("soffice not found")
}

// wordDocToDocxVBS is executed by cscript. PowerShell COM calls often fail with
// "parameter should be System.Management.Automation.PSReference" because Word's
// optional parameters are ByRef; VBScript matches VBA calling convention.
const wordDocToDocxVBS = `Option Explicit
Dim w, d, src, dst
src = WScript.Arguments(0)
dst = WScript.Arguments(1)
On Error Resume Next
Set w = CreateObject("Word.Application")
If Err.Number <> 0 Then
  WScript.StdErr.WriteLine "Word.Application: " & Err.Description
  WScript.Quit 1
End If
w.Visible = False
w.DisplayAlerts = 0
Err.Clear
Set d = w.Documents.Open(src, False, True, False)
If Err.Number <> 0 Then
  WScript.StdErr.WriteLine "Documents.Open: " & Err.Description
  w.Quit 0
  WScript.Quit 1
End If
Err.Clear
d.SaveAs2 dst, 12
If Err.Number <> 0 Then
  WScript.StdErr.WriteLine "SaveAs2: " & Err.Description
  d.Close 0
  w.Quit 0
  WScript.Quit 1
End If
d.Close 0
w.Quit 0
WScript.Quit 0
`

func convertDocToDocxWordCOM(absDoc, absDocx string) error {
	f, err := os.CreateTemp("", "word-doc2docx-*.vbs")
	if err != nil {
		return fmt.Errorf("temp vbs: %w", err)
	}
	vbsPath := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(vbsPath) }()

	if err := os.WriteFile(vbsPath, []byte(wordDocToDocxVBS), 0600); err != nil {
		return fmt.Errorf("write vbs: %w", err)
	}

	cmd := exec.Command("cscript.exe", "//Nologo", "//B", vbsPath, absDoc, absDocx)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
