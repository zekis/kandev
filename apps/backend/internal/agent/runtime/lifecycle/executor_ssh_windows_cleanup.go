package lifecycle

import "strings"

func windowsRemoveTreeScript() string {
	return `$target=[IO.Path]::GetFullPath($target).TrimEnd('\')
$pending=New-Object 'Collections.Generic.Stack[string]'
$directories=New-Object 'Collections.Generic.List[string]'
$files=New-Object 'Collections.Generic.List[string]'
$pending.Push($target)
while ($pending.Count -gt 0) {
 $directory=$pending.Pop()
 $item=Get-Item -LiteralPath $directory -Force
 if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {throw 'Reparse point in cleanup tree'}
 $directories.Add($directory)
 foreach ($child in Get-ChildItem -LiteralPath $directory -Force) {
  $path=[IO.Path]::GetFullPath($child.FullName)
  if (-not $path.StartsWith($target+'\',[StringComparison]::OrdinalIgnoreCase)) {throw 'Cleanup escaped its root'}
  if (($child.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {throw 'Reparse point in cleanup tree'}
  if ($child.PSIsContainer) {$pending.Push($path)} else {$files.Add($path)}
 }
}
foreach ($file in $files) {Remove-Item -LiteralPath $file -Force}
for ($index=$directories.Count-1;$index -ge 0;$index--) {Remove-Item -LiteralPath $directories[$index] -Force}`
}

func windowsOrphanInventoryCommand(root string) string {
	return windowsPowerShellCommand(`$root=` + windowsPowerShellQuote(strings.TrimRight(root, "/")+"/tasks") + `
if (-not (Test-Path -LiteralPath $root -PathType Container)) { exit 0 }
$owner=[Security.Principal.WindowsIdentity]::GetCurrent().User.Value
foreach ($task in Get-ChildItem -LiteralPath $root -Directory -Filter 'task-*') {
 $sessions=Join-Path $task.FullName '.kandev/sessions'
 if (-not (Test-Path -LiteralPath $sessions -PathType Container)) { continue }
 foreach ($session in Get-ChildItem -LiteralPath $sessions -Directory) {
  try {
   $identity=[IO.File]::ReadAllText((Join-Path $session.FullName 'agentctl.identity.json')) | ConvertFrom-Json
   $process=Get-CimInstance Win32_Process -Filter ('ProcessId='+[int]$identity.pid)
   if (-not $process) { continue }
   $sid=Invoke-CimMethod -InputObject $process -MethodName GetOwnerSid
   if ($sid.ReturnValue -ne 0 -or $sid.Sid -ne $owner -or $identity.owner -ne $owner) { continue }
   if ($process.CreationDate.ToUniversalTime().ToString('o') -ne $identity.created -or $process.ExecutablePath -ne $identity.executable) { continue }
   if ([IO.Path]::GetFileName($process.ExecutablePath) -notlike 'agentctl*.exe') {continue}
   $workspace=$task.FullName.Replace('\','/')
   if ($identity.workspace.Replace('\','/').TrimEnd('/') -ne $workspace) {continue}
   [Console]::WriteLine(('PROC'+[char]9+$process.ProcessId+[char]9+$process.ParentProcessId+[char]9+'agentctl.exe --workdir "'+$workspace+'"'))
   $recorded=[IO.File]::ReadAllText((Join-Path $session.FullName 'agentctl.pid')).Trim()
   [Console]::WriteLine(('PIDFILE'+[char]9+$task.Name+[char]9+$session.Name+[char]9+$recorded))
  } catch {
   [Console]::WriteLine(('PIDFILE'+[char]9+$task.Name+[char]9+$session.Name+[char]9+'invalid'))
  }
 }
}`)
}

func windowsReclaimChildReposCommand(task string) string {
	return windowsPowerShellCommand(`foreach ($child in Get-ChildItem -LiteralPath ` + windowsPowerShellQuote(task) + ` -Directory -Force) {
 if (($child.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {throw 'Reparse point in task workspace'}
 $git=Join-Path $child.FullName '.git'
 if (Test-Path -LiteralPath $git) {[Console]::WriteLine($git.Replace('\','/'))}
}`)
}

func windowsReclaimRemoveCommand(task string) string {
	return windowsPowerShellCommand(`$target=[IO.Path]::GetFullPath(` + windowsPowerShellQuote(task) + `)
if ($target -notmatch '[\\/]tasks[\\/][^\\/]+$') {throw 'Not a managed task directory'}
$item=Get-Item -LiteralPath $target -Force
if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {throw 'Task is a reparse point'}
` + windowsRemoveTreeScript())
}
