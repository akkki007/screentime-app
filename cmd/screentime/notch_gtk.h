//go:build gtk3

#ifndef SCREENTIME_NOTCH_H
#define SCREENTIME_NOTCH_H

typedef struct {
	double x, y, w, h;
} nt_rect;

// What the draw handler paints. Go fills it on the GTK main thread and the
// draw handler reads it there, so it needs no lock. Strings are owned by the
// scene (nt_setstr).
typedef struct {
	char *icon, *text, *detail;
	double progress; // focus progress 0..1, or < 0 for none
	double w, h;     // current size of the notch, shoulders included
	int panel;       // draw the panel under the pill
	char *total;
	int napps;
	char *app_name[4], *app_time[4];
	int app_pct[4];
	nt_rect header, rows[4], btns[3];
	char *btn[3];
} nt_scene_t;

extern nt_scene_t nt_scene;

enum { NT_EV_CLICK = 1, NT_EV_ESCAPE = 2, NT_EV_FOCUS_OUT = 3 };
enum { NT_LAYER_SHELL = 1, NT_RGBA = 2 };

// nt_create makes the window. max_w and max_h are the largest the notch gets:
// a layer surface is made that size once and never resized.
int nt_create(double max_w, double max_h);
void nt_apply(double w, double h);
void nt_show(int visible);
void nt_destroy(void);
void nt_set_open(int open);
void nt_redraw(void);
void nt_tick(int on);
int nt_animations(void);
double nt_text_width(const char *s, double px, int bold);
void nt_setstr(char **dst, const char *s);

#endif
