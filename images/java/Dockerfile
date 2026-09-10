FROM eclipse-temurin:21-jre-alpine

# Pre-warm JVM class-data sharing so the first run isn't cold
RUN mkdir -p /warmup && \
    echo 'public class Warmup { public static void main(String[] a) { System.out.println("warm"); } }' > /warmup/Warmup.java && \
    javac /warmup/Warmup.java -d /warmup && \
    java -cp /warmup Warmup && \
    rm -rf /warmup

WORKDIR /workspace

CMD ["sh", "-c", "javac Main.java 2>&1 && java Main"]
