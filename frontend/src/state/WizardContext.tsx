import {
  createContext,
  type Dispatch,
  type PropsWithChildren,
  type RefObject,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useReducer,
  useRef,
} from "react";

import { api, describeError } from "../backend/api";
import { useI18n } from "../i18n";
import { initialWizardState, wizardReducer, type WizardAction, type WizardState } from "./wizardReducer";

interface SecretStore {
  keyRef: RefObject<string>;
  setApiKey: (value: string) => void;
  clearApiKey: () => void;
}

interface WizardContextValue {
  state: WizardState;
  dispatch: Dispatch<WizardAction>;
  secret: SecretStore;
  refreshStatus: () => Promise<void>;
}

const WizardContext = createContext<WizardContextValue | null>(null);

export function WizardProvider({ children }: PropsWithChildren) {
  const { t } = useI18n();
  const [state, dispatch] = useReducer(wizardReducer, initialWizardState);
  const keyRef = useRef("");

  const setApiKey = useCallback((value: string) => {
    keyRef.current = value;
    dispatch({ type: "SET_HAS_API_KEY", value: Boolean(value) });
  }, []);

  const clearApiKey = useCallback(() => {
    keyRef.current = "";
    dispatch({ type: "SET_HAS_API_KEY", value: false });
  }, []);

  const refreshStatus = useCallback(async () => {
    dispatch({ type: "STATUS_LOADING" });
    try {
      dispatch({ type: "STATUS_LOADED", status: await api.status() });
    } catch (error) {
      dispatch({ type: "STATUS_FAILED", message: describeError(error, t("无法读取本机状态")).message });
    }
  }, [t]);

  useEffect(() => {
    void refreshStatus();
  }, [refreshStatus]);

  const value = useMemo<WizardContextValue>(
    () => ({
      state,
      dispatch,
      secret: { keyRef, setApiKey, clearApiKey },
      refreshStatus,
    }),
    [clearApiKey, refreshStatus, setApiKey, state],
  );

  return <WizardContext.Provider value={value}>{children}</WizardContext.Provider>;
}

export function useWizard(): WizardContextValue {
  const value = useContext(WizardContext);
  if (!value) {
    throw new Error("useWizard must be used inside WizardProvider");
  }
  return value;
}
