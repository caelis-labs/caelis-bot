param([string]$Directory,[string]$Output)
$ErrorActionPreference='Stop'
if(-not [IO.Path]::IsPathFullyQualified($Directory) -or -not [IO.Path]::IsPathFullyQualified($Output)){throw 'Absolute archive paths required'}
Add-Type -AssemblyName System.IO.Compression.FileSystem
[IO.Compression.ZipFile]::CreateFromDirectory($Directory,$Output,[IO.Compression.CompressionLevel]::Optimal,$false)
