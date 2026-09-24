"""Pinned dmgbuild layout; creates Finder metadata without automating Finder."""
import os

application = defines['app']
app_name = os.path.basename(application)
format = 'UDZO'
filesystem = 'HFS+'
files = [application]
symlinks = {'Applications': '/Applications'}
icon = os.path.join(application, 'Contents', 'Resources', 'CaelisBot.icns')
background = defines['background']
window_rect = ((240, 180), (660, 430))
default_view = 'icon-view'
show_toolbar = False
show_status_bar = False
show_tab_view = False
show_pathbar = False
show_sidebar = False
include_icon_view_settings = True
include_list_view_settings = False
arrange_by = None
grid_offset = (0, 0)
grid_spacing = 100
scroll_position = (0, 0)
label_pos = 'bottom'
text_size = 14
icon_size = 128
icon_locations = {app_name: (170, 220), 'Applications': (490, 220)}
# Do not write FinderInfo to the signed app (even to hide its extension).
hide_extensions = []
