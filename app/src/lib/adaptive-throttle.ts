/**
 * adaptiveThrottle — coalesce uma rajada de gatilhos em execuções espaçadas, com
 * o espaçamento proporcional ao CUSTO medido de cada execução.
 *
 * Por que existe (diagnóstico de 2026-11-13, diária de 28,8 mil jobs): cada
 * evento do WebSocket disparava, com debounce fixo de ~30 ms, um trabalho que
 * custava centenas de ms (recalcular o canvas inteiro, baixar o pool de
 * condições, baixar 500 alertas). Com jobs terminando em sequência os gatilhos
 * nunca paravam, o trabalho rodava em laço e a aba ficava com a thread principal
 * 100% ocupada. Debounce fixo não resolve: o valor certo para 200 jobs é errado
 * para 200 mil.
 *
 * Regra: depois de uma execução que levou T ms, a próxima só começa após
 * max(minMs, T × idleFactor). Com idleFactor=3 o trabalho ocupa no máximo ~25%
 * do tempo, seja qual for o volume — a tela continua respondendo e o dado
 * exibido atrasa no máximo alguns T. Nunca há duas execuções em voo e o último
 * gatilho sempre gera uma execução (trailing), então nada se perde.
 */
export interface AdaptiveThrottleOptions {
  /** Espaçamento mínimo entre execuções (ms). */
  minMs: number;
  /**
   * Janela de coalescência ANTES de cada execução (ms): do primeiro gatilho até
   * a execução, para uma rajada sair numa execução só. Default = minMs.
   */
  leadMs?: number;
  /** Teto do espaçamento (ms) — limita o atraso do dado exibido. Default 5000. */
  maxMs?: number;
  /** Quantas vezes o custo medido a thread deve ficar livre depois. Default 3. */
  idleFactor?: number;
  /**
   * Incluir no custo o trabalho que a execução DEIXOU AGENDADO na thread
   * principal. Um listener que só faz `setState` retorna em microssegundos, mas
   * o React renderiza depois — e é o render (recalcular o canvas do dia) que
   * custa. Com `settle`, o custo só fecha quando dois timers encadeados
   * conseguem rodar, o que só acontece depois que o render e os efeitos
   * pendentes liberam a thread. Em aba oculta os timers são limitados a ~1 s
   * pelo browser: o ritmo cai sozinho, que é o desejável em segundo plano.
   */
  settle?: boolean;
}

/** Resolve quando a thread principal volta a atender timers (2 voltas). */
export function mainThreadSettled(): Promise<void> {
  return new Promise((resolve) => setTimeout(() => setTimeout(resolve, 0), 0));
}

export function adaptiveThrottle(fn: () => void | Promise<unknown>, opts: AdaptiveThrottleOptions): () => void {
  const maxMs = opts.maxMs ?? 5000;
  const idleFactor = opts.idleFactor ?? 3;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let running = false;
  let pending = false;
  let nextAllowedAt = 0;

  const now = () => (typeof performance !== "undefined" ? performance.now() : Date.now());

  const arm = () => {
    if (timer !== undefined || running) return;
    timer = setTimeout(run, Math.max(opts.leadMs ?? opts.minMs, nextAllowedAt - now()));
  };

  const finish = (startedAt: number) => {
    const cost = now() - startedAt;
    nextAllowedAt = now() + Math.min(maxMs, Math.max(opts.minMs, cost * idleFactor));
    running = false;
    if (pending) arm();
  };

  function run() {
    timer = undefined;
    pending = false;
    running = true;
    const startedAt = now();
    let result: void | Promise<unknown>;
    try {
      result = fn();
    } catch (err) {
      console.error("[adaptive-throttle] task failed", err);
      finish(startedAt);
      return;
    }
    const done = () => {
      if (opts.settle) void mainThreadSettled().then(() => finish(startedAt));
      else finish(startedAt);
    };
    if (result && typeof (result as Promise<unknown>).then === "function") {
      (result as Promise<unknown>).then(done, done);
    } else {
      done();
    }
  }

  return () => {
    pending = true;
    arm();
  };
}
