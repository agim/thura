import { useState, useEffect } from "react";
import { h, btn, avatar } from "../lib/ui.js";

export default function Meet({ notice, event }) {
  const [mic, setMic] = useState(true),
    [camera, setCamera] = useState(false),
    [joined, setJoined] = useState(false),
    [code, setCode] = useState("launch-review"),
    [error, setError] = useState("");
  useEffect(() => {
    if (event) {
      setCode(event.title.toLowerCase().replace(/[^a-z0-9]+/g, "-"));
      setJoined(false);
    }
  }, [event]);
  return h(
    "div",
    { className: "ws-page" },
    h(
      "div",
      { className: "pagehead" },
      h(
        "div",
        null,
        h("h2", null, "Meet"),
        h("p", { className: "sub" }, "A little face time goes a long way."),
      ),
      h(
        "span",
        { className: "tag" },
        joined ? "Demo room joined" : "Ready when you are",
      ),
    ),
    h(
      "div",
      { className: "meeting-layout" },
      h(
        "div",
        null,
        h(
          "div",
          { className: "video-stage" },
          avatar("Demo User"),
          h("h3", null, "Demo User"),
          h(
            "p",
            { className: "tiny muted" },
            camera ? "Camera preview placeholder" : "Camera off",
          ),
          joined
            ? h("span", { className: "tag" }, "You’re in the demo room")
            : null,
        ),
        h(
          "div",
          { className: "video-controls" },
          btn(
            mic ? "Mic on" : "Mic off",
            () => setMic(!mic),
            mic ? "mic" : "mic-off",
            mic ? "secondary" : "selected",
            { "aria-pressed": mic },
          ),
          btn(
            camera ? "Camera on" : "Camera off",
            () => setCamera(!camera),
            camera ? "video" : "video-off",
            camera ? "selected" : "secondary",
            { "aria-pressed": camera },
          ),
          joined
            ? btn(
                "Leave meeting",
                () => {
                  setJoined(false);
                  notice("Left the demo room");
                },
                "phone-off",
                "primary",
              )
            : null,
        ),
      ),
      h(
        "section",
        { className: "meeting-note" },
        h(
          "h3",
          null,
          joined
            ? event?.title || "Website launch review"
            : "Your next conversation",
        ),
        h(
          "p",
          { className: "tiny muted" },
          event
            ? event.title + " · " + event.time + "–" + event.end
            : "Website launch review · 2:30–3:00 PM",
        ),
        h(
          "div",
          { className: "flex", style: { marginBottom: 18 } },
          avatar("Sarah Chen"),
          avatar("Marcus Williams", "gold"),
          h("span", { className: "tiny" }, "Sarah, Marcus, and you"),
        ),
        h(
          "label",
          { className: "tiny" },
          "Meeting name",
          h("input", {
            value: code,
            onChange: (e) => setCode(e.target.value),
            style: { width: "100%", margin: "7px 0 14px" },
          }),
        ),
        btn(
          joined ? "In meeting" : "Join meeting",
          () => {
            if (!code.trim()) {
              setError("Enter a meeting name.");
              return;
            }
            setError("");
            setJoined(true);
            notice(
              "Joined demo room; no camera, microphone, or network connection started",
            );
          },
          "video",
          "primary",
          { disabled: joined },
        ),
        error ? h("p", { className: "error", role: "alert" }, error) : null,
        h(
          "p",
          { className: "tiny muted", style: { marginTop: 18 } },
          "Preview only · camera and microphone controls are simulated.",
        ),
      ),
    ),
  );
}
