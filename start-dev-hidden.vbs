Set fso = CreateObject("Scripting.FileSystemObject")
Set shell = CreateObject("WScript.Shell")
repoDir = fso.GetParentFolderName(WScript.ScriptFullName)
batPath = fso.BuildPath(repoDir, "start-dev.bat")
argStr = ""
For i = 0 To WScript.Arguments.Count - 1
  argStr = argStr & " """ & Replace(WScript.Arguments(i), """", """""") & """"
Next
rc = shell.Run("cmd.exe /c call """ & batPath & """" & argStr, 0, True)
If rc <> 0 Then
  shell.Popup "Server Manager dev launcher failed (exit " & rc & "). See %LOCALAPPDATA%\ServerManager\dev-launcher.log", 8, "Server Manager (Hot-Reload)", 16
End If
