"""Verify shipped Finder metadata, not just the source layout settings."""
import sys
from pathlib import Path
from ds_store import DSStore

root = Path(sys.argv[1])
with DSStore.open(str(root / '.DS_Store'), 'r') as store:
    window = store['.']['bwsp']
    view = store['.']['icvp']
    assert window['WindowBounds'] == '{{240, 180}, {660, 430}}', window
    assert not any(window[key] for key in ('ShowToolbar', 'ShowSidebar', 'ShowStatusBar', 'ShowPathbar'))
    assert view['iconSize'] == 128 and view['textSize'] == 14, view
    assert view['backgroundType'] == 2 and view['backgroundImageAlias']
    assert store['Caelis Bot.app']['Iloc'] == (170, 220)
    assert store['Applications']['Iloc'] == (490, 220)
assert (root / '.background.tiff').is_file()
assert (root / 'Applications').is_symlink()
assert (root / 'Applications').readlink() == Path('/Applications')
print('Verified compact Finder layout, 128-point icons, background and Applications shortcut.')
