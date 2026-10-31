//go:build gtk3

// The notch window: a GtkWindow with one GtkDrawingArea, drawn with cairo and
// Pango (ADR 14). Go decides what to show and where things go (notch_layout.go)
// and fills nt_scene; this file turns that into pixels and forwards clicks and
// keys back. Every function here runs on the GTK main thread.
//
// libgtk-layer-shell is optional: it is dlopen'ed on Wayland only, and without
// it the notch is an ordinary keep-above window.

#include <dlfcn.h>
#include <math.h>
#include <stdlib.h>
#include <string.h>

#include <gdk/gdk.h>
#include <gdk/gdkkeysyms.h>
#include <gtk/gtk.h>

#include "_cgo_export.h"
#include "notch_gtk.h"

// Mirrors of the geometry in notch_layout.go that the drawing needs.
#define SHOULDER 10.0
#define CORNER 14.0
#define ICON 14.0
#define ICON_GAP 8.0
#define PILL_H 34.0

nt_scene_t nt_scene = {.progress = -1};

static GtkWidget *win, *area;
static double origin_x(void);
static int layer_shell, rgba, on_wayland;
// A layer surface keeps one size for good: the compositor drops the keyboard
// from an on-demand surface whose size changes, which would lose Escape the
// moment the panel opens. The notch is drawn inside it and input is limited to
// what is drawn. Other windows are resized instead.
static double fixed_w, fixed_h;
static guint tick_id, rearm_id;
static GdkSeat *grabbed;

// Where the notch's left edge is in the window.
static double origin_x(void) { return layer_shell ? floor((fixed_w - nt_scene.w) / 2) : 0; }

// ---- libgtk-layer-shell, loaded if present -------------------------------

static int (*ls_supported)(void);
static void (*ls_init)(GtkWindow *);
static void (*ls_set_layer)(GtkWindow *, int);
static void (*ls_set_anchor)(GtkWindow *, int, gboolean);
static void (*ls_set_zone)(GtkWindow *, int);
static void (*ls_set_ns)(GtkWindow *, const char *);
static void (*ls_set_kbd)(GtkWindow *, int);

static int load_layer_shell(void) {
	void *h = dlopen("libgtk-layer-shell.so.0", RTLD_LAZY | RTLD_GLOBAL);
	if (!h) return 0;
	ls_supported = dlsym(h, "gtk_layer_is_supported");
	ls_init = dlsym(h, "gtk_layer_init_for_window");
	ls_set_layer = dlsym(h, "gtk_layer_set_layer");
	ls_set_anchor = dlsym(h, "gtk_layer_set_anchor");
	ls_set_zone = dlsym(h, "gtk_layer_set_exclusive_zone");
	ls_set_ns = dlsym(h, "gtk_layer_set_namespace");
	ls_set_kbd = dlsym(h, "gtk_layer_set_keyboard_mode"); // 0.6 and later; optional
	if (!ls_supported || !ls_init || !ls_set_layer || !ls_set_anchor || !ls_set_zone) return 0;
	return ls_supported();
}

// ---- drawing -------------------------------------------------------------

static void set_rgb(cairo_t *cr, unsigned c) {
	cairo_set_source_rgb(cr, ((c >> 16) & 255) / 255.0, ((c >> 8) & 255) / 255.0, (c & 255) / 255.0);
}

// The notch's outline: flat on top, concave shoulders where it meets the top
// edge, rounded bottom corners.
static void shape_path(cairo_t *cr, double w, double h) {
	double s = SHOULDER, r = CORNER;
	cairo_new_path(cr);
	cairo_move_to(cr, 0, 0);
	cairo_line_to(cr, w, 0);
	cairo_arc_negative(cr, w, s, s, -M_PI / 2, -M_PI);
	cairo_line_to(cr, w - s, h - r);
	cairo_arc(cr, w - s - r, h - r, r, 0, M_PI / 2);
	cairo_line_to(cr, s + r, h);
	cairo_arc(cr, s + r, h - r, r, M_PI / 2, M_PI);
	cairo_line_to(cr, s, s);
	cairo_arc_negative(cr, 0, s, s, 0, -M_PI / 2);
	cairo_close_path(cr);
}

static void round_rect(cairo_t *cr, double x, double y, double w, double h, double r) {
	cairo_new_sub_path(cr);
	cairo_arc(cr, x + w - r, y + r, r, -M_PI / 2, 0);
	cairo_arc(cr, x + w - r, y + h - r, r, 0, M_PI / 2);
	cairo_arc(cr, x + r, y + h - r, r, M_PI / 2, M_PI);
	cairo_arc(cr, x + r, y + r, r, M_PI, 3 * M_PI / 2);
	cairo_close_path(cr);
}

// A layout in the system UI font at an absolute pixel size.
static PangoLayout *text_layout(const char *s, double px, int bold) {
	PangoLayout *l = gtk_widget_create_pango_layout(area, s ? s : "");
	PangoFontDescription *fd = pango_font_description_copy(pango_context_get_font_description(gtk_widget_get_pango_context(area)));
	pango_font_description_set_absolute_size(fd, px * PANGO_SCALE);
	pango_font_description_set_weight(fd, bold ? PANGO_WEIGHT_SEMIBOLD : PANGO_WEIGHT_NORMAL);
	pango_layout_set_font_description(l, fd);
	pango_font_description_free(fd);
	return l;
}

double nt_text_width(const char *s, double px, int bold) {
	PangoLayout *l = text_layout(s, px, bold);
	int w, h;
	pango_layout_get_pixel_size(l, &w, &h);
	g_object_unref(l);
	return w;
}

// Text with its left edge at x (align 0), right edge at x (1) or centre at x
// (2), vertically centred on cy.
static double draw_text(cairo_t *cr, const char *s, double x, double cy, double px, int bold, unsigned col, int align) {
	if (!s || !*s) return 0;
	PangoLayout *l = text_layout(s, px, bold);
	int w, h;
	pango_layout_get_pixel_size(l, &w, &h);
	double left = align == 1 ? x - w : align == 2 ? x - w / 2.0 : x;
	set_rgb(cr, col);
	cairo_move_to(cr, left, cy - h / 2.0);
	pango_cairo_show_layout(cr, l);
	g_object_unref(l);
	return w;
}

// Icons are small paths in a 14 px box centred on (cx, cy), white, 1.6 px
// strokes with round ends so they stay crisp at 1x and 2x.
static void draw_icon(cairo_t *cr, const char *name, double cx, double cy) {
	cairo_save(cr);
	cairo_translate(cr, cx, cy);
	set_rgb(cr, 0xffffff);
	cairo_set_line_width(cr, 1.6);
	cairo_set_line_cap(cr, CAIRO_LINE_CAP_ROUND);
	cairo_set_line_join(cr, CAIRO_LINE_JOIN_ROUND);
	if (!strcmp(name, "clock")) {
		cairo_arc(cr, 0, 0, 6, 0, 2 * M_PI);
		cairo_stroke(cr);
		cairo_move_to(cr, 0, -3.4);
		cairo_line_to(cr, 0, 0);
		cairo_line_to(cr, 2.6, 1.6);
		cairo_stroke(cr);
	} else if (!strcmp(name, "timer")) {
		cairo_arc(cr, 0, 1, 5.2, 0, 2 * M_PI);
		cairo_stroke(cr);
		cairo_move_to(cr, -2, -6);
		cairo_line_to(cr, 2, -6);
		cairo_move_to(cr, 0, 1);
		cairo_line_to(cr, 0, -2);
		cairo_stroke(cr);
	} else if (!strcmp(name, "pause")) {
		cairo_set_line_width(cr, 2.4);
		cairo_set_line_cap(cr, CAIRO_LINE_CAP_BUTT);
		cairo_move_to(cr, -2.6, -4.5);
		cairo_line_to(cr, -2.6, 4.5);
		cairo_move_to(cr, 2.6, -4.5);
		cairo_line_to(cr, 2.6, 4.5);
		cairo_stroke(cr);
	} else if (!strcmp(name, "limit")) {
		cairo_arc(cr, 0, 0, 6, 0, 2 * M_PI);
		cairo_stroke(cr);
		cairo_move_to(cr, -4.2, -4.2);
		cairo_line_to(cr, 4.2, 4.2);
		cairo_stroke(cr);
	} else if (!strcmp(name, "break")) {
		cairo_rectangle(cr, -5.5, -1.5, 8, 7);
		cairo_stroke(cr);
		cairo_arc(cr, 2.5, 1.8, 2.4, -M_PI / 2, M_PI / 2);
		cairo_stroke(cr);
		cairo_move_to(cr, -3.5, -5);
		cairo_line_to(cr, -3.5, -3.5);
		cairo_move_to(cr, -0.5, -5);
		cairo_line_to(cr, -0.5, -3.5);
		cairo_stroke(cr);
	} else if (!strcmp(name, "moon")) {
		// A disc with a second disc taken out of it.
		cairo_save(cr);
		cairo_set_fill_rule(cr, CAIRO_FILL_RULE_EVEN_ODD);
		cairo_rectangle(cr, -10, -10, 20, 20);
		cairo_arc(cr, 3.4, -2.8, 5.2, 0, 2 * M_PI);
		cairo_clip(cr);
		cairo_new_path(cr);
		cairo_arc(cr, 0, 0, 6, 0, 2 * M_PI);
		cairo_fill(cr);
		cairo_restore(cr);
	} else if (!strcmp(name, "bolt")) {
		cairo_move_to(cr, 1.6, -6.5);
		cairo_line_to(cr, -4, 1);
		cairo_line_to(cr, -0.4, 1);
		cairo_line_to(cr, -1.6, 6.5);
		cairo_line_to(cr, 4, -1.5);
		cairo_line_to(cr, 0.4, -1.5);
		cairo_close_path(cr);
		cairo_fill(cr);
	} else if (!strcmp(name, "wave")) {
		static const double fx[4] = {-3, -1, 1, 3}, fy[4] = {-4.4, -6, -6, -4.4};
		for (int i = 0; i < 4; i++) {
			cairo_move_to(cr, fx[i], -1);
			cairo_line_to(cr, fx[i], fy[i]);
		}
		cairo_move_to(cr, -4, -1);
		cairo_line_to(cr, -4, 1);
		cairo_arc_negative(cr, 0, 1, 4, M_PI, 0);
		cairo_line_to(cr, 4, -1);
		cairo_stroke(cr);
		cairo_move_to(cr, -4, 1);
		cairo_line_to(cr, -6, -1);
		cairo_stroke(cr);
	} else { // offline
		cairo_arc(cr, 0, 0, 6, 0, 2 * M_PI);
		cairo_stroke(cr);
		cairo_move_to(cr, 0, -3);
		cairo_line_to(cr, 0, 0.6);
		cairo_stroke(cr);
		cairo_arc(cr, 0, 3.2, 0.5, 0, 2 * M_PI);
		cairo_fill(cr);
	}
	cairo_restore(cr);
}

static void draw_button(cairo_t *cr, nt_rect r, const char *label) {
	round_rect(cr, r.x, r.y, r.w, r.h, 8);
	set_rgb(cr, 0x1c1917);
	cairo_fill(cr);
	draw_text(cr, label, r.x + r.w / 2, r.y + r.h / 2, 12.5, 1, 0xffffff, 2);
}

static void draw_panel(cairo_t *cr) {
	nt_scene_t *s = &nt_scene;
	draw_text(cr, "Today", s->header.x, s->header.y + 8, 11, 0, 0xa8a29e, 0);
	draw_text(cr, s->total, s->header.x, s->header.y + 30, 24, 1, 0xffffff, 0);
	if (s->napps == 0) {
		draw_text(cr, "No activity yet", s->header.x, s->rows[0].y + s->rows[0].h / 2, 13, 0, 0xa8a29e, 0);
	}
	for (int i = 0; i < s->napps; i++) {
		nt_rect r = s->rows[i];
		draw_text(cr, s->app_time[i], r.x + r.w, r.y + 9, 12, 0, 0xa8a29e, 1);
		draw_text(cr, s->app_name[i], r.x, r.y + 9, 13, 0, 0xffffff, 0);
		round_rect(cr, r.x, r.y + 19, r.w, 3, 1.5);
		set_rgb(cr, 0x292524);
		cairo_fill(cr);
		double fw = r.w * s->app_pct[i] / 100.0;
		if (fw > 0) {
			if (fw < 3) fw = 3;
			round_rect(cr, r.x, r.y + 19, fw, 3, 1.5);
			set_rgb(cr, 0xe7e5e4);
			cairo_fill(cr);
		}
	}
	for (int i = 0; i < 3; i++) draw_button(cr, s->btns[i], s->btn[i]);
}

static gboolean on_draw(GtkWidget *w, cairo_t *cr, gpointer data) {
	nt_scene_t *s = &nt_scene;
	if (s->w < 1) return FALSE;
	cairo_set_operator(cr, CAIRO_OPERATOR_CLEAR);
	cairo_paint(cr);
	cairo_set_operator(cr, CAIRO_OPERATOR_OVER);
	cairo_translate(cr, origin_x(), 0);
	shape_path(cr, s->w, s->h);
	set_rgb(cr, 0x0c0a09);
	cairo_fill(cr);

	cairo_save(cr);
	shape_path(cr, s->w, s->h);
	cairo_clip(cr);

	// The pill row: icon, text and detail, centred in the pill.
	double body = s->w - 2 * SHOULDER, cy = (PILL_H - 2) / 2.0 + 0.5;
	double tw = 0, dw = 0;
	if (s->text && *s->text) tw = nt_text_width(s->text, 13, 1);
	if (s->detail && *s->detail) dw = nt_text_width(s->detail, 12, 0) + ICON_GAP;
	double x = SHOULDER + (body - (ICON + ICON_GAP + tw + dw)) / 2.0;
	draw_icon(cr, s->icon ? s->icon : "clock", x + ICON / 2, cy);
	x += ICON + ICON_GAP;
	draw_text(cr, s->text, x, cy, 13, 1, 0xffffff, 0);
	if (dw > 0) draw_text(cr, s->detail, x + tw + ICON_GAP, cy, 12, 0, 0xa8a29e, 0);

	if (s->progress >= 0) { // the focus hairline along the pill's bottom edge
		double x0 = SHOULDER + CORNER, len = (body - 2 * CORNER) * s->progress;
		if (len > 0) {
			cairo_rectangle(cr, x0, PILL_H - 2, len, 2);
			set_rgb(cr, 0xff8f45);
			cairo_fill(cr);
		}
	}
	if (s->panel) draw_panel(cr);
	cairo_restore(cr);
	return FALSE;
}

// ---- input ---------------------------------------------------------------

static gboolean on_button(GtkWidget *w, GdkEventButton *e, gpointer data) {
	if (e->button == 1 && e->type == GDK_BUTTON_PRESS) ntEvent(NT_EV_CLICK, e->x - origin_x(), e->y);
	return TRUE;
}

static gboolean on_key(GtkWidget *w, GdkEventKey *e, gpointer data) {
	if (e->keyval == GDK_KEY_Escape) {
		ntEvent(NT_EV_ESCAPE, 0, 0);
		return TRUE;
	}
	return FALSE;
}

static gboolean on_focus_out(GtkWidget *w, GdkEventFocus *e, gpointer data) {
	ntEvent(NT_EV_FOCUS_OUT, 0, 0);
	return FALSE;
}

static gboolean on_grab_broken(GtkWidget *w, GdkEvent *e, gpointer data) {
	grabbed = NULL;
	ntEvent(NT_EV_FOCUS_OUT, 0, 0);
	return FALSE;
}

static gboolean on_tick(GtkWidget *w, GdkFrameClock *fc, gpointer data) {
	if (ntTick(gdk_frame_clock_get_frame_time(fc))) return G_SOURCE_CONTINUE;
	tick_id = 0;
	return G_SOURCE_REMOVE;
}

// ---- window --------------------------------------------------------------

int nt_create(double max_w, double max_h) {
	int flags = 0;
	fixed_w = ceil(max_w);
	fixed_h = ceil(max_h);
	GdkDisplay *dpy = gdk_display_get_default();
	if (!dpy) return -1;
	on_wayland = !strcmp(G_OBJECT_TYPE_NAME(dpy), "GdkWaylandDisplay");

	win = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	gtk_window_set_title(GTK_WINDOW(win), "Screentime notch");
	gtk_window_set_decorated(GTK_WINDOW(win), FALSE);
	gtk_window_set_skip_taskbar_hint(GTK_WINDOW(win), TRUE);
	gtk_window_set_skip_pager_hint(GTK_WINDOW(win), TRUE);
	gtk_window_set_keep_above(GTK_WINDOW(win), TRUE);
	gtk_window_stick(GTK_WINDOW(win));
	gtk_window_set_type_hint(GTK_WINDOW(win), GDK_WINDOW_TYPE_HINT_DOCK);
	gtk_window_set_default_size(GTK_WINDOW(win), 160, PILL_H);
	gtk_widget_set_app_paintable(win, TRUE);

	// A transparent window needs an RGBA visual and a compositor to use it.
	GdkScreen *screen = gtk_widget_get_screen(win);
	GdkVisual *vis = gdk_screen_get_rgba_visual(screen);
	if (vis && gdk_screen_is_composited(screen)) {
		gtk_widget_set_visual(win, vis);
		rgba = 1;
		flags |= NT_RGBA;
	}
	GtkCssProvider *css = gtk_css_provider_new();
	gtk_css_provider_load_from_data(css, "window, window.background { background: transparent; box-shadow: none; }", -1, NULL);
	gtk_style_context_add_provider(gtk_widget_get_style_context(win), GTK_STYLE_PROVIDER(css), GTK_STYLE_PROVIDER_PRIORITY_APPLICATION);
	g_object_unref(css);

	if (on_wayland && load_layer_shell()) {
		ls_init(GTK_WINDOW(win));
		ls_set_layer(GTK_WINDOW(win), 2); // TOP
		ls_set_anchor(GTK_WINDOW(win), 2, TRUE); // TOP; unanchored left and right, so it is centred
		ls_set_zone(GTK_WINDOW(win), 0);
		if (ls_set_ns) ls_set_ns(GTK_WINDOW(win), "screentime-notch");
		layer_shell = 1;
		flags |= NT_LAYER_SHELL;
	}

	area = gtk_drawing_area_new();
	gtk_container_add(GTK_CONTAINER(win), area);
	if (layer_shell) gtk_widget_set_size_request(area, (int)fixed_w, (int)fixed_h);
	gtk_widget_add_events(win, GDK_BUTTON_PRESS_MASK | GDK_KEY_PRESS_MASK | GDK_FOCUS_CHANGE_MASK);
	g_signal_connect(area, "draw", G_CALLBACK(on_draw), NULL);
	g_signal_connect(win, "button-press-event", G_CALLBACK(on_button), NULL);
	g_signal_connect(win, "key-press-event", G_CALLBACK(on_key), NULL);
	g_signal_connect(win, "focus-out-event", G_CALLBACK(on_focus_out), NULL);
	g_signal_connect(win, "grab-broken-event", G_CALLBACK(on_grab_broken), NULL);
	gtk_widget_show(area);
	gtk_widget_realize(win);
	return flags;
}

// Row inset at y of the outline: how far the shape is from the window's
// left edge there. Used to cut the window to shape without a compositor.
static double inset_at(double y, double h) {
	double s = SHOULDER, r = CORNER;
	if (y < s) return sqrt(fmax(0, s * s - (s - y) * (s - y)));
	if (y > h - r) return s + r - sqrt(fmax(0, r * r - (y - (h - r)) * (y - (h - r))));
	return s;
}

void nt_apply(double w, double h) {
	nt_scene.w = w;
	nt_scene.h = h;
	int iw = (int)ceil(w), ih = (int)ceil(h);
	if (!layer_shell) {
		gtk_widget_set_size_request(area, iw, ih);
		gtk_window_resize(GTK_WINDOW(win), iw, ih);
	}
	GdkWindow *gw = gtk_widget_get_window(win);
	if (gw) {
		// Clicks on the transparent shoulders fall through to what is below.
		cairo_rectangle_int_t body = {(int)(origin_x() + SHOULDER), 0, iw - 2 * (int)SHOULDER, ih};
		cairo_region_t *in = cairo_region_create_rectangle(&body);
		gdk_window_input_shape_combine_region(gw, in, 0, 0);
		cairo_region_destroy(in);
		if (!rgba) {
			cairo_region_t *shape = cairo_region_create();
			for (int y = 0; y < ih; y++) {
				double in_x = inset_at(y + 0.5, h);
				cairo_rectangle_int_t row = {(int)ceil(in_x), y, iw - 2 * (int)ceil(in_x), 1};
				cairo_region_union_rectangle(shape, &row);
			}
			gdk_window_shape_combine_region(gw, shape, 0, 0);
			cairo_region_destroy(shape);
		}
	}
	if (!layer_shell && !on_wayland) {
		GdkMonitor *m = gdk_display_get_primary_monitor(gdk_display_get_default());
		if (!m) m = gdk_display_get_monitor(gdk_display_get_default(), 0);
		if (m) {
			GdkRectangle g;
			gdk_monitor_get_geometry(m, &g);
			gtk_window_move(GTK_WINDOW(win), g.x + (g.width - iw) / 2, g.y);
		}
	}
	gtk_widget_queue_draw(area);
}

static gboolean rearm_keyboard(gpointer data);

// nt_destroy removes the window and frees what the scene holds.
void nt_destroy(void) {
	nt_set_open(0);
	if (rearm_id) g_source_remove(rearm_id);
	rearm_id = 0;
	if (win) gtk_widget_destroy(win); // takes the tick callback with it
	win = area = NULL;
	tick_id = 0;
	layer_shell = rgba = 0;
	nt_setstr(&nt_scene.icon, NULL);
	nt_setstr(&nt_scene.text, NULL);
	nt_setstr(&nt_scene.detail, NULL);
	nt_setstr(&nt_scene.total, NULL);
	for (int i = 0; i < 4; i++) {
		nt_setstr(&nt_scene.app_name[i], NULL);
		nt_setstr(&nt_scene.app_time[i], NULL);
	}
	for (int i = 0; i < 3; i++) nt_setstr(&nt_scene.btn[i], NULL);
	memset(&nt_scene, 0, sizeof nt_scene);
	nt_scene.progress = -1;
}

void nt_show(int visible) {
	if (visible) {
		gtk_widget_show(win);
		if (layer_shell && ls_set_kbd && !rearm_id) rearm_id = g_timeout_add(150, rearm_keyboard, NULL);
	} else {
		nt_set_open(0);
		gtk_widget_hide(win);
	}
}

// A layer surface only gets the keyboard from the click that happens while it
// is on-demand, so it stays on-demand while collapsed. Closing the panel drops
// to none to hand the keyboard back, and a one-shot a moment later restores
// on-demand (both in one commit would change nothing).
static gboolean rearm_keyboard(gpointer data) {
	rearm_id = 0;
	if (win && ls_set_kbd) ls_set_kbd(GTK_WINDOW(win), 2);
	return G_SOURCE_REMOVE;
}

// While the panel is open it wants the keyboard (Escape) and, on X11, a grab
// so that a click anywhere else closes it; a dock window is never focused by
// the window manager, so focus-out alone would not.
void nt_set_open(int open) {
	if (layer_shell && ls_set_kbd) {
		if (!open) {
			ls_set_kbd(GTK_WINDOW(win), 0); // NONE
			if (!rearm_id) rearm_id = g_timeout_add(150, rearm_keyboard, NULL);
		}
	}
	if (on_wayland) return;
	GdkWindow *gw = gtk_widget_get_window(win);
	if (open && !grabbed && gw && gtk_widget_get_visible(win)) {
		GdkSeat *seat = gdk_display_get_default_seat(gdk_display_get_default());
		if (gdk_seat_grab(seat, gw, GDK_SEAT_CAPABILITY_ALL, TRUE, NULL, NULL, NULL, NULL) == GDK_GRAB_SUCCESS) grabbed = seat;
	} else if (!open && grabbed) {
		gdk_seat_ungrab(grabbed);
		grabbed = NULL;
	}
}

void nt_redraw(void) { gtk_widget_queue_draw(area); }

// The tick callback runs once per frame, only while an animation does.
void nt_tick(int on) {
	if (on && !tick_id) tick_id = gtk_widget_add_tick_callback(area, on_tick, NULL, NULL);
	if (!on && tick_id) {
		gtk_widget_remove_tick_callback(area, tick_id);
		tick_id = 0;
	}
}

int nt_animations(void) {
	gboolean on = TRUE;
	g_object_get(gtk_settings_get_default(), "gtk-enable-animations", &on, NULL);
	return on;
}

void nt_setstr(char **dst, const char *s) {
	free(*dst);
	*dst = s ? strdup(s) : NULL;
}
