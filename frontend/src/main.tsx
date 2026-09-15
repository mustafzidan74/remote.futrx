import { render } from "preact";
import { LocaleRoot } from "./i18n/LocaleRoot";
import { localeStore } from "./i18n/localeStore";
import "./index.css";
import "./i18n/arabic.css";

// Before the first render: every element's text is translated as it is created.
localeStore.boot();

const root = document.getElementById("root")!;
render(<LocaleRoot />, root);
