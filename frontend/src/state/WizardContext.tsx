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

import { api, describeError } from "../api/client";
import { initialWizardState, wizardReducer, type WizardAction, type WizardState } from "./wizardReducer";

interface SecretStore {
  keyRef: RefObject<string>;
  setApiKey: (value: string) => void;
  clearApiKey: () => void;
  // registerField hands the input element to the store so clearApiKey can empty
  // it. The field is uncontrolled, so that node is where the key actually lives.
  registerField: (node: HTMLInputElement | null) => void;
}

interface WizardContextValue {
  state: WizardState;
  dispatch: Dispatch<WizardAction>;
  secret: SecretStore;
  refreshStatus: () => Promise<void>;
}

const WizardContext = createContext<WizardContextValue | null>(null);

export function WizardProvider({ children }: PropsWithChildren) {
  const [state, dispatch] = useReducer(wizardReducer, initialWizardState);
  const keyRef = useRef("");

  const setApiKey = useCallback((value: string) => {
    keyRef.current = value;
    dispatch({ type: "SET_HAS_API_KEY", value: Boolean(value) });
  }, []);

  // The field is uncontrolled, so the DOM node holds the only copy of the
  // characters. It registers itself here so clearApiKey can empty it -- without
  // this, clearing reset the ref while the key stayed visible on screen.
  const fieldRef = useRef<HTMLInputElement | null>(null);
  const registerField = useCallback((node: HTMLInputElement | null) => {
    fieldRef.current = node;
  }, []);

  const clearApiKey = useCallback(() => {
    keyRef.current = "";
    if (fieldRef.current) {
      fieldRef.current.value = "";
    }
    dispatch({ type: "SET_HAS_API_KEY", value: false });
  }, []);

  const refreshStatus = useCallback(async () => {
    dispatch({ type: "STATUS_LOADING" });
    try {
      dispatch({ type: "STATUS_LOADED", status: await api.status() });
    } catch (error) {
      dispatch({ type: "STATUS_FAILED", message: describeError(error, "无法读取本机状态").message });
    }
  }, []);

  useEffect(() => {
    void refreshStatus();
  }, [refreshStatus]);

  const value = useMemo<WizardContextValue>(
    () => ({
      state,
      dispatch,
      secret: { keyRef, setApiKey, clearApiKey, registerField },
      refreshStatus,
    }),
    [clearApiKey, refreshStatus, registerField, setApiKey, state],
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
