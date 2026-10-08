import React, { useState } from "react";
import { h, btn } from "../lib/ui.js";
import { useStoredState } from "../lib/storage.js";

const eventSeed = [
  {
    id: 1,
    title: "Team check-in",
    date: "2026-10-06",
    time: "09:00",
    end: "09:30",
    type: "Work",
    location: "Thura Meet",
  },
  {
    id: 2,
    title: "Launch review",
    date: "2026-10-06",
    time: "14:30",
    end: "15:00",
    type: "Work",
    location: "Thura Meet",
  },
  {
    id: 3,
    title: "Client visit",
    date: "2026-10-07",
    time: "10:00",
    end: "11:00",
    type: "Work",
    location: "White Marsh",
  },
  {
    id: 4,
    title: "Build time",
    date: "2026-10-08",
    time: "13:00",
    end: "15:00",
    type: "Focus",
    location: "Office",
  },
  {
    id: 5,
    title: "Family dinner",
    date: "2026-10-09",
    time: "18:00",
    end: "19:00",
    type: "Personal",
    location: "Home",
  },
  {
    id: 6,
    title: "Project planning",
    date: "2026-10-13",
    time: "11:00",
    end: "12:00",
    type: "Work",
    location: "Thura Meet",
  },
  {
    id: 7,
    title: "Design handoff",
    date: "2026-10-15",
    time: "10:00",
    end: "11:00",
    type: "Work",
    location: "Thura Meet",
  },
  {
    id: 8,
    title: "Build time",
    date: "2026-10-20",
    time: "13:00",
    end: "15:00",
    type: "Focus",
    location: "Office",
  },
  {
    id: 9,
    title: "Launch day",
    date: "2026-10-22",
    time: "09:00",
    end: "10:00",
    type: "Work",
    location: "Thura Meet",
  },
];
const dateKey = (d) =>
  d.getFullYear() +
  "-" +
  String(d.getMonth() + 1).padStart(2, "0") +
  "-" +
  String(d.getDate()).padStart(2, "0");
const timeLabel = (t) => {
  const [hh, mm] = t.split(":").map(Number);
  return (
    (hh % 12 || 12) +
    (mm ? ":" + String(mm).padStart(2, "0") : "") +
    (hh < 12 ? "am" : "pm")
  );
};
export default function Calendar({ notice, openMeet }) {
  const [month, setMonth] = useState(new Date(2026, 9, 1)),
    [view, setView] = useState("Month"),
    [events, setEvents] = useStoredState("events", eventSeed),
    [filter, setFilter] = useState("All"),
    [edit, setEdit] = useState(null),
    [selectedEvent, setSelectedEvent] = useState(null),
    [error, setError] = useState("");
  const monthPrefix = dateKey(month).slice(0, 7);
  const shown = events
    .filter(
      (e) =>
        e.date.startsWith(monthPrefix) &&
        (filter === "All" || e.type === filter),
    )
    .sort((a, b) => (a.date + a.time).localeCompare(b.date + b.time));
  const start = new Date(month.getFullYear(), month.getMonth(), 1);
  start.setDate(1 - ((start.getDay() + 6) % 7));
  const days = Array.from(
    {
      length:
        35 +
        (new Date(month.getFullYear(), month.getMonth() + 1, 0).getDate() +
          ((new Date(month.getFullYear(), month.getMonth(), 1).getDay() + 6) %
            7) >
        35
          ? 7
          : 0),
    },
    (_, i) => {
      const d = new Date(start);
      d.setDate(start.getDate() + i);
      return d;
    },
  );
  function newEvent(date = dateKey(new Date(2026, 9, 6))) {
    setSelectedEvent(null);
    setError("");
    setEdit({
      title: "",
      date,
      time: "09:00",
      end: "10:00",
      type: "Work",
      location: "Thura Meet",
    });
  }
  function save() {
    if (
      !edit.title.trim() ||
      !edit.date ||
      !edit.time ||
      !edit.end ||
      edit.end <= edit.time
    ) {
      setError("Add a title, date, and an end time after the start time.");
      return;
    }
    setEvents((es) => [
      ...es.filter((e) => e.id !== edit.id),
      { ...edit, id: edit.id || Date.now() },
    ]);
    setMonth(new Date(edit.date + "T12:00:00"));
    setEdit(null);
    notice("Event saved to the demo calendar");
  }
  return h(
    "div",
    { className: "ws-page" },
    h(
      "div",
      { className: "pagehead" },
      h(
        "div",
        null,
        h("h2", null, "Calendar"),
        h("p", { className: "sub" }, "Make room for what matters."),
      ),
      btn("Create event", () => newEvent(), "plus", "primary"),
    ),
    edit
      ? h(
          "section",
          { className: "inline-form" },
          h(
            "div",
            { className: "flex spread" },
            h("h3", null, edit.id ? "Edit event" : "New event"),
            btn("Close", () => setEdit(null), "x"),
          ),
          h(
            "div",
            { className: "form-grid" },
            [
              ["title", "Event title", "text"],
              ["date", "Date", "date"],
              ["time", "Start time", "time"],
              ["end", "End time", "time"],
              ["location", "Location", "text"],
            ].map(([k, label, type]) =>
              h(
                "label",
                { key: k },
                label,
                h("input", {
                  type,
                  value: edit[k],
                  onChange: (e) => setEdit({ ...edit, [k]: e.target.value }),
                }),
              ),
            ),
            h(
              "label",
              null,
              "Calendar",
              h(
                "select",
                {
                  value: edit.type,
                  onChange: (e) => setEdit({ ...edit, type: e.target.value }),
                },
                ["Work", "Personal", "Focus"].map((t) =>
                  h("option", { key: t }, t),
                ),
              ),
            ),
          ),
          error ? h("p", { className: "error", role: "alert" }, error) : null,
          btn("Save event", save, "check", "primary"),
        )
      : null,
    selectedEvent
      ? h(
          "section",
          { className: "inline-form" },
          h(
            "div",
            { className: "flex spread" },
            h(
              "div",
              null,
              h(
                "span",
                {
                  className:
                    "tag " +
                    (selectedEvent.type === "Personal"
                      ? "coral"
                      : selectedEvent.type === "Focus"
                        ? "gold"
                        : ""),
                },
                selectedEvent.type,
              ),
              h("h3", null, selectedEvent.title),
            ),
            btn("Close", () => setSelectedEvent(null), "x"),
          ),
          h(
            "p",
            { style: { margin: "12px 0" } },
            selectedEvent.date +
              " · " +
              timeLabel(selectedEvent.time) +
              "–" +
              timeLabel(selectedEvent.end) +
              " · " +
              selectedEvent.location,
          ),
          h(
            "div",
            { className: "flex" },
            btn(
              "Edit",
              () => {
                setEdit({ ...selectedEvent });
                setSelectedEvent(null);
                setError("");
              },
              "square-pen",
              "secondary",
            ),
            selectedEvent.location === "Thura Meet"
              ? btn(
                  "Open meeting",
                  () => openMeet(selectedEvent),
                  "video",
                  "primary",
                )
              : null,
            btn(
              "Delete",
              () => {
                setEvents((es) => es.filter((e) => e.id !== selectedEvent.id));
                setSelectedEvent(null);
                notice("Event deleted");
              },
              "trash-2",
            ),
          ),
        )
      : null,
    h(
      "div",
      { className: "calendar-area" },
      h(
        "div",
        { className: "flex spread", style: { marginBottom: 18 } },
        h(
          "div",
          { className: "flex" },
          btn(
            null,
            () =>
              setMonth(new Date(month.getFullYear(), month.getMonth() - 1, 1)),
            "chevron-left",
            "",
            { "aria-label": "Previous month" },
          ),
          h(
            "h3",
            null,
            month.toLocaleDateString("en-US", {
              month: "long",
              year: "numeric",
            }),
          ),
          btn(
            null,
            () =>
              setMonth(new Date(month.getFullYear(), month.getMonth() + 1, 1)),
            "chevron-right",
            "",
            { "aria-label": "Next month" },
          ),
          btn("Today", () => setMonth(new Date(2026, 9, 1)), null, "secondary"),
        ),
        h(
          "div",
          { className: "flex" },
          h(
            "select",
            {
              "aria-label": "Calendar filter",
              value: filter,
              onChange: (e) => setFilter(e.target.value),
            },
            ["All", "Work", "Personal", "Focus"].map((t) =>
              h("option", { key: t }, t),
            ),
          ),
          ["Month", "Agenda"].map((v) =>
            btn(v, () => setView(v), null, view === v ? "selected" : "", {
              key: v,
              "aria-pressed": view === v,
            }),
          ),
        ),
      ),
      view === "Month"
        ? h(
            "div",
            { className: "cal-grid" },
            ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"].map((d) =>
              h("div", { key: d, className: "weekday" }, d),
            ),
            days.map((d) => {
              const key = dateKey(d);
              return h(
                "div",
                {
                  key,
                  className:
                    "day " +
                    (d.getMonth() !== month.getMonth() ? "outside" : ""),
                },
                btn(
                  d.getDate(),
                  () => newEvent(key),
                  null,
                  "day-number " + (key === "2026-10-06" ? "today" : ""),
                  { "aria-label": "Create event on " + key },
                ),
                events
                  .filter(
                    (e) =>
                      e.date === key && (filter === "All" || e.type === filter),
                  )
                  .map((e) =>
                    btn(
                      h(
                        React.Fragment,
                        null,
                        h(
                          "span",
                          { className: "event-time" },
                          timeLabel(e.time) + " ",
                        ),
                        e.title,
                      ),
                      () => {
                        setSelectedEvent(e);
                        setEdit(null);
                      },
                      null,
                      "event-chip " + e.type.toLowerCase(),
                      {
                        key: e.id,
                        "aria-label": e.title + " " + timeLabel(e.time),
                        "data-tooltip":
                          e.title +
                          " · " +
                          timeLabel(e.time) +
                          "–" +
                          timeLabel(e.end),
                      },
                    ),
                  ),
              );
            }),
          )
        : h(
            "div",
            { className: "agenda" },
            shown.length
              ? shown.map((e) =>
                  h(
                    "div",
                    { key: e.id, className: "agenda-row" },
                    h(
                      "strong",
                      null,
                      new Date(e.date + "T12:00:00").toLocaleDateString(
                        "en-US",
                        { month: "short", day: "numeric" },
                      ),
                    ),
                    h(
                      "span",
                      { className: "tiny muted" },
                      timeLabel(e.time) + "–" + timeLabel(e.end),
                    ),
                    h(
                      "div",
                      null,
                      h("strong", null, e.title),
                      h("div", { className: "sub" }, e.location),
                    ),
                    btn(
                      "Details",
                      () => setSelectedEvent(e),
                      "arrow-up-right",
                      "secondary",
                    ),
                  ),
                )
              : h(
                  "div",
                  { className: "empty-space" },
                  "No events in this view.",
                ),
          ),
      h(
        "div",
        { className: "flex spread", style: { marginTop: 15 } },
        h(
          "div",
          { className: "flex" },
          h("span", { className: "tag" }, "Work"),
          h("span", { className: "tag coral" }, "Personal"),
          h("span", { className: "tag gold" }, "Focus"),
        ),
        h(
          "span",
          { className: "tiny muted" },
          "America/New_York · Sample schedule",
        ),
      ),
    ),
  );
}
