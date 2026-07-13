Option Explicit

Dim shell, fileSystem, scriptDirectory, launcherPath

Set shell = CreateObject("WScript.Shell")
Set fileSystem = CreateObject("Scripting.FileSystemObject")

scriptDirectory = fileSystem.GetParentFolderName(WScript.ScriptFullName)
launcherPath = fileSystem.BuildPath(scriptDirectory, "build\bin\palserver-launcher.exe")

If Not fileSystem.FileExists(launcherPath) Then
    MsgBox "Launcher executable was not found:" & vbCrLf & launcherPath, vbCritical, "Palserver Launcher"
    WScript.Quit 1
End If

shell.CurrentDirectory = fileSystem.GetParentFolderName(launcherPath)
shell.Run Chr(34) & launcherPath & Chr(34), 1, False
