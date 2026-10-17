param(
  [switch]$SkipBuild,
	[switch]$ReplaceLocalDraft,
  [ValidateSet('development','preview')][string]$Channel='development',
  [string]$ApplicationDirectory='',
  [ValidateRange(0,65535)][int]$PackageRevision=1,
  [string]$CertificateThumbprint='',
  [string]$Publisher='CN=Caelis Labs',
  [string]$TimestampURL='http://timestamp.digicert.com'
)
. "$PSScriptRoot/windows-sdk.ps1"
if ($env:BOT_RELEASE_TAG) { throw 'MSIX publication is deferred; use the NSIS preview packaging path' }
if (-not $SkipBuild) { & "$PSScriptRoot/build-windows.ps1" -Channel $Channel -OutputDirectory $ApplicationDirectory }
$app = if($ApplicationDirectory){[IO.Path]::GetFullPath($ApplicationDirectory)}else{Join-Path $taskBotRoot "dist/windows-$Channel"}
$artifactChannel = if ($Channel -eq 'release') {'stable'} else {$Channel}
$build = Get-Content -Raw -LiteralPath (Join-Path $app 'build.json') | ConvertFrom-Json
if ($build.channel -ne $artifactChannel -or $build.platform -ne 'windows-amd64') { throw 'Build identity mismatch' }
if ((Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $app 'caelis-bot.exe')).Hash.ToLowerInvariant() -ne $build.sha256) { throw 'App changed after build' }
if ($env:BOT_RELEASE_TAG -and (-not $CertificateThumbprint -or $build.dirty -or $build.version -ne $env:BOT_RELEASE_TAG.Substring(1))) { throw 'Published packages require exact tagged source and a selected signing certificate' }
$name = switch ($Channel) { 'preview' {'Caelis Bot Preview'} 'release' {'Caelis Bot'} default {'Caelis Bot Dev'} }
$identity = switch ($Channel) { 'preview' {'dev.caelis.bot.preview'} 'release' {'dev.caelis.bot'} default {'dev.caelis.bot.dev'} }
$version = $build.version.Split('-')[0]
$packageVersion = "$version.$PackageRevision"
$work = Join-Path $taskBotRoot ('.cache/msix-' + [guid]::NewGuid().ToString('N'))
$stage = Join-Path $work 'package'
New-Item -ItemType Directory -Path $work | Out-Null
try {
  @{ info = @{companyName='Caelis Labs';productName=$name;productIdentifier=$identity;description='A quiet desktop companion';version=$version} } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $work 'wails.json') -Encoding utf8NoBOM
  # Pinned Wails produces the base package. Its current MSIX command copies only
  # the exe: augment that standard layout, then repack with the same SDK tool.
  Invoke-BotCommand go run "github.com/wailsapp/wails/v3/cmd/wails3@$($taskBotPins.wails)" tool msix --config (Join-Path $work 'wails.json') --publisher $Publisher --arch x64 --name caelis-bot.exe --executable (Join-Path $app 'caelis-bot.exe') --out (Join-Path $work 'base.msix') --use-makeappx
  Invoke-BotCommand makeappx.exe unpack /p (Join-Path $work 'base.msix') /d $stage /o
  # Include the same pinned Helpers, Plugins and licenses as Setup/ZIP.
  Copy-Item -Path (Join-Path $app '*') -Destination $stage -Recurse -Force
  Invoke-BotCommand go run ./cmd/windows-package-assets internal/desktop/assets/app-icon.png (Join-Path $stage 'Assets')
  $manifest = Join-Path $stage 'AppxManifest.xml'
  [xml]$xml = Get-Content -Raw -LiteralPath $manifest
  $xml.Package.Identity.Version = $packageVersion
	$xml.Package.Dependencies.TargetDeviceFamily.MinVersion = '10.0.19045.0'
  $xml.Package.Dependencies.TargetDeviceFamily.MaxVersionTested = '10.0.26100.0'
  # AppData must remain outside MSIX's copy-on-write store. Otherwise uninstall
  # can delete the Notebook and receipts, and the external CLI sees other bytes.
  $desktop6='http://schemas.microsoft.com/appx/manifest/desktop/windows10/6'
  $rescap='http://schemas.microsoft.com/appx/manifest/foundation/windows10/restrictedcapabilities'
  $xml.Package.SetAttribute('xmlns:desktop6',$desktop6)
  $xml.Package.SetAttribute('xmlns:rescap',$rescap)
  $xml.Package.SetAttribute('IgnorableNamespaces',(($xml.Package.GetAttribute('IgnorableNamespaces')+' desktop6 rescap').Trim()))
  $virtualization=$xml.CreateElement('desktop6','FileSystemWriteVirtualization',$desktop6)
  $virtualization.InnerText='disabled'
  $xml.Package.Properties.AppendChild($virtualization) | Out-Null
  $capability=$xml.CreateElement('rescap','Capability',$rescap)
  $capability.SetAttribute('Name','unvirtualizedResources')
  $xml.Package.Capabilities.AppendChild($capability) | Out-Null
  $xml.Save($manifest)
  # Unpack-generated blockmap/metadata describe old bytes; MakeAppx regenerates.
  foreach ($generated in @('AppxBlockMap.xml','[Content_Types].xml','AppxSignature.p7x')) {
    $path=Join-Path $stage $generated
    if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Force }
  }
  $releaseDir = Join-Path $taskBotRoot 'dist/releases'
  New-Item -ItemType Directory -Force -Path $releaseDir | Out-Null
  $suffix = if ($env:BOT_RELEASE_TAG) {"-$artifactChannel"} else {"-$artifactChannel-local"}
  $package = Join-Path $releaseDir "Caelis-Bot-$($build.version)-windows-amd64$suffix.msix"
  if (Test-Path -LiteralPath $package) {
    if ($env:BOT_RELEASE_TAG -or -not $ReplaceLocalDraft) { throw 'Package already exists; use -ReplaceLocalDraft for a local draft only. Published bytes are immutable.' }
    $resolved=[IO.Path]::GetFullPath($package)
    $allowed=[IO.Path]::GetFullPath($releaseDir)+[IO.Path]::DirectorySeparatorChar
    if (-not $resolved.StartsWith($allowed,[StringComparison]::OrdinalIgnoreCase) -or -not $resolved.EndsWith('-local.msix',[StringComparison]::OrdinalIgnoreCase)) { throw 'Local draft path rejected' }
    foreach($localFile in @($resolved,"$resolved.sha256","$resolved.build.json")) {
      if (Test-Path -LiteralPath $localFile) { Remove-Item -LiteralPath $localFile -Force }
    }
  }
  Invoke-BotCommand makeappx.exe pack /d $stage /p $package /o
  $signed=$false
  if ($CertificateThumbprint) {
    if ($CertificateThumbprint -notmatch '^[a-fA-F0-9]{40}$') { throw 'Invalid certificate thumbprint' }
    $certificate = Get-Item -LiteralPath "Cert:/CurrentUser/My/$CertificateThumbprint"
    if (-not $certificate.HasPrivateKey -or $certificate.Subject -ne $Publisher -or $certificate.NotAfter -le (Get-Date)) { throw 'Publisher/certificate mismatch or certificate unavailable' }
    Invoke-BotCommand signtool.exe sign /fd SHA256 /sha1 $CertificateThumbprint /tr $TimestampURL /td SHA256 $package
    Invoke-BotCommand signtool.exe verify /pa /all /v $package
    $signature = Get-AuthenticodeSignature -LiteralPath $package
    if ($signature.Status -ne 'Valid' -or -not $signature.TimeStamperCertificate) { throw 'Package signature/timestamp rejected' }
    $signed=$true
  }
  $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $package).Hash.ToLowerInvariant()
  "$hash  $([IO.Path]::GetFileName($package))" | Set-Content -LiteralPath "$package.sha256" -Encoding utf8NoBOM
  @{schema=1;version=$build.version;packageVersion=$packageVersion;source=$build.source;dirty=$build.dirty;channel=$artifactChannel;platform='windows-amd64';format='msix';sha256=$hash;signed=$signed;qualified=$false} | ConvertTo-Json | Set-Content -LiteralPath "$package.build.json" -Encoding utf8NoBOM
  Write-Output "Packaged $package; signed=$signed; native/install acceptance required before publication"
} finally {
  $resolved=[IO.Path]::GetFullPath($work)
  $allowed=[IO.Path]::GetFullPath((Join-Path $taskBotRoot '.cache'))+[IO.Path]::DirectorySeparatorChar
  if ($resolved.StartsWith($allowed,[StringComparison]::OrdinalIgnoreCase) -and (Test-Path -LiteralPath $resolved)) { Remove-Item -LiteralPath $resolved -Recurse -Force }
}
